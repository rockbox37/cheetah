use reqwest::Client;
use scraper::{Html, Selector};

#[tokio::main]
async fn main() {
    println!("cheetah-fast worker starting");
}

pub async fn fetch_and_parse(client: &Client, url: &str) -> Result<String, Box<dyn std::error::Error>> {
    let resp = client.get(url).send().await?.text().await?;
    let document = Html::parse_document(&resp);
    let body_selector = Selector::parse("body").unwrap();

    let body_text: String = document
        .select(&body_selector)
        .flat_map(|el| el.text())
        .collect::<Vec<_>>()
        .join("\n");

    Ok(body_text)
}
