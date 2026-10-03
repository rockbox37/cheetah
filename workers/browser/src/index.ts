import { chromium, type Browser } from "playwright";
import Redis from "ioredis";
import TurndownService from "turndown";
import { URL } from "node:url";
import { promises as dns } from "node:dns";

const STREAM = process.env.STREAM ?? "browser_jobs";
const GROUP = process.env.GROUP ?? "browser-workers";
const REDIS_URL = process.env.REDIS_URL ?? "redis://127.0.0.1:6379";
const RESULTS_PREFIX = process.env.RESULTS_PREFIX ?? "job_results";
const CONCURRENCY = parseInt(process.env.CONCURRENCY ?? "5", 10);
const MAX_BODY_BYTES = 10 * 1024 * 1024;
const PAGE_TIMEOUT_MS = 30_000;
const RESULT_TTL_SECONDS = 3600;

interface JobMessage {
  job_id: string;
  url: string;
  format?: string;
  resolved_ip?: string;
}

interface StatusPayload {
  job_id: string;
  status: "completed" | "failed";
  pages_total: number;
  pages_completed: number;
  results: ResultEntry[];
}

interface ResultEntry {
  success: boolean;
  data: {
    content: string;
    markdown: string;
    metadata: {
      title: string;
      description: string;
      language: string;
      status_code: number;
    };
  };
}

const turndown = new TurndownService({
  headingStyle: "atx",
  codeBlockStyle: "fenced",
  bulletListMarker: "-",
});
(turndown as any).remove(["script", "style", "nav", "footer", "aside", "noscript", "svg"]);

export function isPrivateIP(ip: string): boolean {
  if (ip.includes(":")) {
    const lower = ip.toLowerCase();
    if (lower === "::1" || lower === "::") return true;
    if (lower.startsWith("fe80:") || lower.startsWith("fc") || lower.startsWith("fd")) return true;
    const v4mapped = lower.match(/^::ffff:(\d+\.\d+\.\d+\.\d+)$/);
    if (v4mapped) return isPrivateIP(v4mapped[1]);
    const v4hex = lower.match(/^::ffff:([0-9a-f]{1,4}):([0-9a-f]{1,4})$/);
    if (v4hex) {
      const hi = parseInt(v4hex[1], 16);
      const lo = parseInt(v4hex[2], 16);
      return isPrivateIP(`${hi >> 8}.${hi & 0xff}.${lo >> 8}.${lo & 0xff}`);
    }
    return false;
  }
  const parts = ip.split(".").map(Number);
  if (parts.length !== 4 || parts.some((p) => isNaN(p))) return true;
  const [a, b] = parts;
  if (a === 10) return true;
  if (a === 172 && b >= 16 && b <= 31) return true;
  if (a === 192 && b === 168) return true;
  if (a === 127) return true;
  if (a === 0) return true;
  if (a === 169 && b === 254) return true;
  if (a >= 224) return true;
  return false;
}

export async function validateURL(rawURL: string, resolvedIP?: string): Promise<void> {
  const parsed = new URL(rawURL);
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new Error("SSRF: only http and https schemes are allowed");
  }

  let hostname = parsed.hostname;
  if (hostname.startsWith("[") && hostname.endsWith("]")) {
    hostname = hostname.slice(1, -1);
  }
  if (/^\d+\.\d+\.\d+\.\d+$/.test(hostname) || hostname.includes(":")) {
    if (isPrivateIP(hostname)) {
      throw new Error("SSRF: URL hostname is a private/reserved IP");
    }
  }

  if (!resolvedIP) {
    throw new Error("SSRF: resolved_ip is required");
  }
  if (isPrivateIP(resolvedIP)) {
    throw new Error("SSRF: resolved IP is private/reserved");
  }
}

export async function renderPage(
  browser: Browser,
  url: string,
): Promise<{ html: string; title: string; description: string; language: string; statusCode: number }> {
  const context = await browser.newContext({
    userAgent: "Mozilla/5.0 (compatible; Cheetah/1.0; +https://cheetah.dev/bot)",
    bypassCSP: true,
  });
  const page = await context.newPage();
  try {
    await context.route("**/*", async (route) => {
      const reqURL = route.request().url();
      try {
        const reqParsed = new URL(reqURL);
        let host = reqParsed.hostname;
        if (host.startsWith("[") && host.endsWith("]")) host = host.slice(1, -1);
        if (/^\d+\.\d+\.\d+\.\d+$/.test(host) || host.includes(":")) {
          if (isPrivateIP(host)) {
            await route.abort("blockedbyclient");
            return;
          }
        } else {
          const addrs = await dns.resolve4(host).catch(() => [] as string[]);
          if (addrs.some(isPrivateIP)) {
            await route.abort("blockedbyclient");
            return;
          }
        }
      } catch {
        await route.abort("blockedbyclient");
        return;
      }
      await route.continue();
    });

    const response = await page.goto(url, {
      waitUntil: "networkidle",
      timeout: PAGE_TIMEOUT_MS,
    });

    const statusCode = response?.status() ?? 0;
    const html = await page.content();

    if (Buffer.byteLength(html, "utf-8") > MAX_BODY_BYTES) {
      throw new Error(`Response body exceeds max size of ${MAX_BODY_BYTES} bytes`);
    }

    const title = (await page.title()) ?? "";
    const description = await page
      .locator('meta[name="description"]')
      .getAttribute("content")
      .catch(() => null) ?? "";
    const language = await page
      .locator("html")
      .getAttribute("lang")
      .catch(() => null) ?? "";

    return { html, title, description, language, statusCode };
  } finally {
    await context.close();
  }
}

export function htmlToMarkdown(html: string): string {
  return turndown.turndown(html);
}

export function parseJob(fields: string[]): JobMessage | null {
  const map: Record<string, string> = {};
  for (let i = 0; i < fields.length; i += 2) {
    map[fields[i]] = fields[i + 1];
  }
  if (!map.job_id || !map.url) return null;
  return {
    job_id: map.job_id,
    url: map.url,
    format: map.format,
    resolved_ip: map.resolved_ip,
  };
}

export function redactURL(rawURL: string): string {
  try {
    const parsed = new URL(rawURL);
    parsed.search = "";
    parsed.hash = "";
    parsed.username = "";
    parsed.password = "";
    return parsed.toString();
  } catch {
    return "<invalid-url>";
  }
}

async function processJob(
  redis: Redis,
  browser: Browser,
  job: JobMessage,
  entryId: string,
): Promise<void> {
  const resultKey = `${RESULTS_PREFIX}:${job.job_id}`;

  try {
    await validateURL(job.url, job.resolved_ip);

    const { html, title, description, language, statusCode } = await renderPage(browser, job.url);
    const wantMarkdown = job.format !== "html";
    const markdown = wantMarkdown ? htmlToMarkdown(html) : "";

    const payload: StatusPayload = {
      job_id: job.job_id,
      status: "completed",
      pages_total: 1,
      pages_completed: 1,
      results: [
        {
          success: true,
          data: {
            content: wantMarkdown ? "" : html,
            markdown,
            metadata: { title, description, language, status_code: statusCode },
          },
        },
      ],
    };

    await redis.set(resultKey, JSON.stringify(payload), "EX", RESULT_TTL_SECONDS);
    console.log(`[ok] job=${job.job_id} url=${redactURL(job.url)} status=${statusCode}`);
  } catch (err) {
    const errMsg = err instanceof Error ? err.message : String(err);
    const tag = errMsg.startsWith("SSRF") ? "ssrf_blocked" : "fetch_failed";
    const payload: StatusPayload = {
      job_id: job.job_id,
      status: "failed",
      pages_total: 1,
      pages_completed: 0,
      results: [],
    };
    try {
      await redis.set(resultKey, JSON.stringify(payload), "EX", RESULT_TTL_SECONDS);
    } catch {
      console.error(`[fail] could not write failure result for job=${job.job_id}`);
    }
    console.error(`[fail] job=${job.job_id} url=${redactURL(job.url)} error=${tag}`);
  } finally {
    await redis.xack(STREAM, GROUP, entryId).catch(() => {});
  }
}

async function main(): Promise<void> {
  const redis = new Redis(REDIS_URL, { maxRetriesPerRequest: null });
  const consumerName = `worker-${crypto.randomUUID()}`;

  try {
    await redis.xgroup("CREATE", STREAM, GROUP, "0", "MKSTREAM");
  } catch {
    // group already exists
  }

  console.log(
    `starting browser worker: stream=${STREAM} group=${GROUP} consumer=${consumerName} concurrency=${CONCURRENCY}`,
  );

  const browser: Browser = await chromium.launch({
    args: ["--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage"],
  });

  let shuttingDown = false;
  const inflight = new Set<Promise<void>>();

  const shutdown = async () => {
    if (shuttingDown) return;
    shuttingDown = true;
    console.log("shutting down, waiting for in-flight jobs...");
    await Promise.allSettled(inflight);
    await browser.close();
    redis.disconnect();
    console.log("shutdown complete");
    process.exit(0);
  };

  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);

  console.log(`worker ready, consuming from stream '${STREAM}'`);

  while (!shuttingDown) {
    if (inflight.size >= CONCURRENCY) {
      await Promise.race(inflight);
      continue;
    }

    let entries: [string, string[]][];
    try {
      const result = await redis.xreadgroup(
        "GROUP", GROUP, consumerName,
        "COUNT", String(Math.max(1, CONCURRENCY - inflight.size)),
        "BLOCK", "2000",
        "STREAMS", STREAM, ">",
      );

      if (!result || (result as any[]).length === 0) continue;
      entries = (result as any)[0][1] as [string, string[]][];
    } catch (err) {
      if (!shuttingDown) {
        console.error(`XREADGROUP failed: ${(err as Error).message ?? String(err)}`);
        await new Promise((r) => setTimeout(r, 1000));
      }
      continue;
    }

    for (const [entryId, fields] of entries) {
      if (shuttingDown) break;
      const job = parseJob(fields);
      if (!job) {
        console.warn(`job missing required fields, skipping: ${entryId}`);
        await redis.xack(STREAM, GROUP, entryId);
        continue;
      }

      const task = processJob(redis, browser, job, entryId)
        .catch((err) => console.error(`[drop] job=${job.job_id} err=${err instanceof Error ? err.message : String(err)}`))
        .finally(() => { inflight.delete(task); });
      inflight.add(task);
    }
  }
}

main().catch((err) => {
  console.error("fatal:", err);
  process.exit(1);
});
