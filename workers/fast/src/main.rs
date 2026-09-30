use cheetah_fast::{fetch_page, FetchConfig};
use clap::Parser;
use reqwest::Client;
use std::sync::Arc;
use tokio::signal;
use tokio::sync::Semaphore;
use tracing::{error, info, warn};

#[derive(Parser, Debug)]
#[command(name = "cheetah-fast", about = "Fast static page scraper worker")]
struct Args {
    #[arg(long, default_value = "redis://127.0.0.1:6379")]
    redis_url: String,

    #[arg(long, default_value_t = 50)]
    concurrency: usize,

    #[arg(long, default_value = "fast-workers")]
    group_name: String,

    #[arg(long, default_value = "scrape_jobs")]
    stream: String,

    #[arg(long, default_value = "job_results")]
    results_prefix: String,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "cheetah_fast=info".into()),
        )
        .init();

    let args = Args::parse();
    let consumer_name = format!("worker-{}", uuid::Uuid::new_v4());

    info!(
        redis = %args.redis_url,
        concurrency = args.concurrency,
        group = %args.group_name,
        consumer = %consumer_name,
        "starting cheetah-fast worker"
    );

    let redis_client = redis::Client::open(args.redis_url.as_str())?;
    let mut conn = redis_client.get_multiplexed_async_connection().await?;

    let _: Result<(), _> = redis::cmd("XGROUP")
        .arg("CREATE")
        .arg(&args.stream)
        .arg(&args.group_name)
        .arg("0")
        .arg("MKSTREAM")
        .query_async(&mut conn)
        .await;

    let http_client = Client::builder()
        .pool_max_idle_per_host(20)
        .redirect(reqwest::redirect::Policy::none())
        .build()?;
    let config = Arc::new(FetchConfig::default());
    let semaphore = Arc::new(Semaphore::new(args.concurrency));

    let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

    tokio::spawn(async move {
        let ctrl_c = signal::ctrl_c();
        let mut sigterm = signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to register SIGTERM handler");

        tokio::select! {
            _ = ctrl_c => info!("received SIGINT, shutting down"),
            _ = sigterm.recv() => info!("received SIGTERM, shutting down"),
        }
        let _ = shutdown_tx.send(true);
    });

    info!("worker ready, consuming from stream '{}'", args.stream);

    loop {
        if *shutdown_rx.borrow() {
            break;
        }

        let result: redis::RedisResult<redis::streams::StreamReadReply> = redis::cmd("XREADGROUP")
            .arg("GROUP")
            .arg(&args.group_name)
            .arg(&consumer_name)
            .arg("COUNT")
            .arg(10)
            .arg("BLOCK")
            .arg(2000_u64)
            .arg("STREAMS")
            .arg(&args.stream)
            .arg(">")
            .query_async(&mut conn)
            .await;

        let reply = match result {
            Ok(r) => r,
            Err(e) => {
                warn!(error = %e, "XREADGROUP failed, retrying");
                tokio::time::sleep(std::time::Duration::from_secs(1)).await;
                continue;
            }
        };

        for stream_key in &reply.keys {
            for entry in &stream_key.ids {
                let entry_id = entry.id.clone();
                let url = match entry.map.get("url") {
                    Some(redis::Value::BulkString(bytes)) => {
                        String::from_utf8_lossy(bytes).to_string()
                    }
                    _ => {
                        warn!(id = %entry_id, "job missing 'url' field, skipping");
                        let _: Result<(), _> = redis::cmd("XACK")
                            .arg(&args.stream)
                            .arg(&args.group_name)
                            .arg(&entry_id)
                            .query_async(&mut conn)
                            .await;
                        continue;
                    }
                };

                let job_id = entry
                    .map
                    .get("job_id")
                    .and_then(|v| match v {
                        redis::Value::BulkString(bytes) => {
                            Some(String::from_utf8_lossy(bytes).to_string())
                        }
                        _ => None,
                    })
                    .unwrap_or_else(|| entry_id.clone());

                let permit = semaphore.clone().acquire_owned().await.unwrap();
                let client = http_client.clone();
                let cfg = config.clone();
                let mut task_conn = conn.clone();
                let stream_name = args.stream.clone();
                let group = args.group_name.clone();
                let results_prefix = args.results_prefix.clone();

                tokio::spawn(async move {
                    let _permit = permit;
                    info!(url = %url, job_id = %job_id, "processing job");

                    let result = fetch_page(&client, &url, &cfg).await;
                    let result_key = format!("{}:{}", results_prefix, job_id);

                    match result {
                        Ok(page) => {
                            let payload = serde_json::to_string(&page).unwrap_or_default();
                            // P1 fix: Store results as individual keys with a 1-hour TTL
                            // instead of hash fields (HSET has no per-field expiry).
                            let _: Result<(), _> = redis::cmd("SET")
                                .arg(&result_key)
                                .arg(&payload)
                                .arg("EX")
                                .arg(3600_u64)
                                .query_async(&mut task_conn)
                                .await;
                            info!(url = %url, job_id = %job_id, status = page.status_code, "job complete");
                        }
                        Err(e) => {
                            let error_payload = serde_json::json!({
                                "job_id": job_id,
                                "url": url,
                                "error": e.to_string(),
                                "fetched_at": chrono::Utc::now(),
                            });
                            let _: Result<(), _> = redis::cmd("SET")
                                .arg(&result_key)
                                .arg(error_payload.to_string())
                                .arg("EX")
                                .arg(3600_u64)
                                .query_async(&mut task_conn)
                                .await;
                            error!(url = %url, job_id = %job_id, error = %e, "job failed");
                        }
                    }

                    let _: Result<(), _> = redis::cmd("XACK")
                        .arg(&stream_name)
                        .arg(&group)
                        .arg(&entry_id)
                        .query_async(&mut task_conn)
                        .await;
                });
            }
        }
    }

    info!("shutdown complete");
    Ok(())
}
