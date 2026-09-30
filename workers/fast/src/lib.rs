use chrono::{DateTime, Utc};
use reqwest::Client;
use scraper::{ElementRef, Html, Node, Selector};
use serde::{Deserialize, Serialize};
use std::fmt::Write as FmtWrite;
use std::time::Duration;

#[derive(Debug, thiserror::Error)]
pub enum Error {
    #[error("HTTP request failed: {0}")]
    Http(#[from] reqwest::Error),
    #[error("Response body exceeds max size of {max_bytes} bytes")]
    BodyTooLarge { max_bytes: usize },
    #[error("Non-success status code: {0}")]
    BadStatus(u16),
}

pub type Result<T> = std::result::Result<T, Error>;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PageResult {
    pub url: String,
    pub title: Option<String>,
    pub description: Option<String>,
    pub language: Option<String>,
    pub status_code: u16,
    pub markdown: String,
    pub raw_html: String,
    pub fetched_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Metadata {
    pub title: Option<String>,
    pub description: Option<String>,
    pub language: Option<String>,
    pub og_title: Option<String>,
    pub og_description: Option<String>,
    pub og_image: Option<String>,
    pub og_url: Option<String>,
}

#[derive(Debug, Clone)]
pub struct FetchConfig {
    pub timeout: Duration,
    pub user_agent: String,
    pub max_body_size: usize,
}

impl Default for FetchConfig {
    fn default() -> Self {
        Self {
            timeout: Duration::from_secs(10),
            user_agent: "Mozilla/5.0 (compatible; Cheetah/1.0; +https://cheetah.dev/bot)".into(),
            max_body_size: 10 * 1024 * 1024,
        }
    }
}

pub async fn fetch_page(client: &Client, url: &str, config: &FetchConfig) -> Result<PageResult> {
    let resp = client
        .get(url)
        .header("User-Agent", &config.user_agent)
        .timeout(config.timeout)
        .send()
        .await?;

    let status_code = resp.status().as_u16();
    let raw_html = resp.text().await?;

    if raw_html.len() > config.max_body_size {
        return Err(Error::BodyTooLarge {
            max_bytes: config.max_body_size,
        });
    }

    let document = Html::parse_document(&raw_html);
    let metadata = extract_metadata(&document);
    let markdown = html_to_markdown(&raw_html);

    Ok(PageResult {
        url: url.to_string(),
        title: metadata.title.or(metadata.og_title),
        description: metadata.description.or(metadata.og_description),
        language: metadata.language,
        status_code,
        markdown,
        raw_html,
        fetched_at: Utc::now(),
    })
}

pub fn extract_metadata(document: &Html) -> Metadata {
    let title = selector_text(document, "title");

    let language = document
        .select(&sel("html"))
        .next()
        .and_then(|el| el.value().attr("lang").map(String::from));

    let description = meta_content(document, "description");
    let og_title = meta_property(document, "og:title");
    let og_description = meta_property(document, "og:description");
    let og_image = meta_property(document, "og:image");
    let og_url = meta_property(document, "og:url");

    Metadata {
        title,
        description,
        language,
        og_title,
        og_description,
        og_image,
        og_url,
    }
}

fn sel(s: &str) -> Selector {
    Selector::parse(s).unwrap()
}

fn selector_text(document: &Html, selector: &str) -> Option<String> {
    document
        .select(&sel(selector))
        .next()
        .map(|el| el.text().collect::<String>().trim().to_string())
        .filter(|s| !s.is_empty())
}

fn meta_content(document: &Html, name: &str) -> Option<String> {
    let selector_str = format!("meta[name=\"{name}\"]");
    document
        .select(&sel(&selector_str))
        .next()
        .and_then(|el| el.value().attr("content").map(String::from))
        .filter(|s| !s.is_empty())
}

fn meta_property(document: &Html, property: &str) -> Option<String> {
    let selector_str = format!("meta[property=\"{property}\"]");
    document
        .select(&sel(&selector_str))
        .next()
        .and_then(|el| el.value().attr("content").map(String::from))
        .filter(|s| !s.is_empty())
}

const STRIP_TAGS: &[&str] = &["script", "style", "nav", "footer", "aside", "noscript", "svg"];

pub fn html_to_markdown(html: &str) -> String {
    let document = Html::parse_document(html);
    let body = match document.select(&sel("body")).next() {
        Some(el) => el,
        None => return String::new(),
    };

    let mut out = String::with_capacity(html.len() / 2);
    let mut state = ConvertState::default();
    convert_children(&body, &mut out, &mut state);
    collapse_blank_lines(&out)
}

#[derive(Default, Clone)]
struct ConvertState {
    list_depth: usize,
    ordered: bool,
    item_counter: usize,
    pre_block: bool,
}

fn convert_children(element: &ElementRef, out: &mut String, state: &mut ConvertState) {
    let mut li_counter: usize = 0;

    for child in element.children() {
        match child.value() {
            Node::Text(text) => {
                let t = text.text.as_ref();
                if state.pre_block {
                    out.push_str(t);
                } else {
                    let collapsed = collapse_whitespace(t);
                    if !collapsed.is_empty() {
                        out.push_str(&collapsed);
                    }
                }
            }
            Node::Element(el) => {
                let tag = el.name();
                if STRIP_TAGS.contains(&tag) {
                    continue;
                }
                let child_ref = ElementRef::wrap(child).unwrap();

                match tag {
                    "h1" => write_heading(&child_ref, out, state, 1),
                    "h2" => write_heading(&child_ref, out, state, 2),
                    "h3" => write_heading(&child_ref, out, state, 3),
                    "h4" => write_heading(&child_ref, out, state, 4),
                    "h5" => write_heading(&child_ref, out, state, 5),
                    "h6" => write_heading(&child_ref, out, state, 6),
                    "p" => {
                        ensure_blank_line(out);
                        convert_children(&child_ref, out, state);
                        ensure_blank_line(out);
                    }
                    "br" => out.push('\n'),
                    "a" => {
                        let href = el.attr("href").unwrap_or("#");
                        out.push('[');
                        convert_children(&child_ref, out, state);
                        out.push_str("](");
                        out.push_str(href);
                        out.push(')');
                    }
                    "img" => {
                        let alt = el.attr("alt").unwrap_or("");
                        let src = el.attr("src").unwrap_or("");
                        let _ = write!(out, "![{alt}]({src})");
                    }
                    "strong" | "b" => {
                        out.push_str("**");
                        convert_children(&child_ref, out, state);
                        out.push_str("**");
                    }
                    "em" | "i" => {
                        out.push('*');
                        convert_children(&child_ref, out, state);
                        out.push('*');
                    }
                    "code" if !state.pre_block => {
                        out.push('`');
                        convert_children(&child_ref, out, state);
                        out.push('`');
                    }
                    "pre" => {
                        ensure_blank_line(out);
                        out.push_str("```\n");
                        let saved_pre = state.pre_block;
                        state.pre_block = true;
                        convert_children(&child_ref, out, state);
                        state.pre_block = saved_pre;
                        if !out.ends_with('\n') {
                            out.push('\n');
                        }
                        out.push_str("```\n");
                    }
                    "ul" => {
                        ensure_blank_line(out);
                        let saved_depth = state.list_depth;
                        let saved_ordered = state.ordered;
                        let saved_counter = state.item_counter;
                        state.list_depth += 1;
                        state.ordered = false;
                        state.item_counter = 0;
                        convert_children(&child_ref, out, state);
                        state.list_depth = saved_depth;
                        state.ordered = saved_ordered;
                        state.item_counter = saved_counter;
                    }
                    "ol" => {
                        ensure_blank_line(out);
                        let saved_depth = state.list_depth;
                        let saved_ordered = state.ordered;
                        let saved_counter = state.item_counter;
                        state.list_depth += 1;
                        state.ordered = true;
                        state.item_counter = 0;
                        convert_children(&child_ref, out, state);
                        state.list_depth = saved_depth;
                        state.ordered = saved_ordered;
                        state.item_counter = saved_counter;
                    }
                    "li" => {
                        li_counter += 1;
                        let indent = "  ".repeat(state.list_depth.saturating_sub(1));
                        if !out.ends_with('\n') && !out.is_empty() {
                            out.push('\n');
                        }
                        if state.ordered {
                            let _ = write!(out, "{indent}{li_counter}. ");
                        } else {
                            let _ = write!(out, "{indent}- ");
                        }
                        convert_children(&child_ref, out, state);
                    }
                    "blockquote" => {
                        ensure_blank_line(out);
                        let mut inner = String::new();
                        convert_children(&child_ref, &mut inner, state);
                        for line in inner.trim().lines() {
                            out.push_str("> ");
                            out.push_str(line);
                            out.push('\n');
                        }
                    }
                    "hr" => {
                        ensure_blank_line(out);
                        out.push_str("---\n");
                    }
                    _ => {
                        convert_children(&child_ref, out, state);
                    }
                }
            }
            _ => {}
        }
    }
}

fn write_heading(element: &ElementRef, out: &mut String, state: &mut ConvertState, level: usize) {
    ensure_blank_line(out);
    for _ in 0..level {
        out.push('#');
    }
    out.push(' ');
    convert_children(element, out, state);
    out.push('\n');
}

fn ensure_blank_line(out: &mut String) {
    if out.is_empty() {
        return;
    }
    if !out.ends_with('\n') {
        out.push('\n');
    }
    if !out.ends_with("\n\n") {
        out.push('\n');
    }
}

fn collapse_whitespace(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut last_was_space = false;
    for ch in s.chars() {
        if ch.is_ascii_whitespace() {
            if !last_was_space {
                result.push(' ');
                last_was_space = true;
            }
        } else {
            result.push(ch);
            last_was_space = false;
        }
    }
    result
}

fn collapse_blank_lines(s: &str) -> String {
    let mut result = String::with_capacity(s.len());
    let mut blank_count = 0;
    for line in s.lines() {
        if line.trim().is_empty() {
            blank_count += 1;
            if blank_count <= 1 {
                result.push('\n');
            }
        } else {
            blank_count = 0;
            result.push_str(line);
            result.push('\n');
        }
    }
    result.trim().to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn headings() {
        let html = "<html><body><h1>Title</h1><h2>Subtitle</h2><h3>Section</h3></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("# Title"));
        assert!(md.contains("## Subtitle"));
        assert!(md.contains("### Section"));
    }

    #[test]
    fn paragraphs() {
        let html = "<html><body><p>First paragraph.</p><p>Second paragraph.</p></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("First paragraph."));
        assert!(md.contains("Second paragraph."));
        let parts: Vec<&str> = md.split("First paragraph.").collect();
        assert!(parts.len() == 2);
    }

    #[test]
    fn links() {
        let html = r#"<html><body><p>Visit <a href="https://example.com">Example</a> now.</p></body></html>"#;
        let md = html_to_markdown(html);
        assert!(md.contains("[Example](https://example.com)"));
    }

    #[test]
    fn images() {
        let html = r#"<html><body><img src="photo.jpg" alt="A photo"></body></html>"#;
        let md = html_to_markdown(html);
        assert!(md.contains("![A photo](photo.jpg)"));
    }

    #[test]
    fn bold_italic() {
        let html = "<html><body><p><strong>bold</strong> and <em>italic</em></p></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("**bold**"));
        assert!(md.contains("*italic*"));
    }

    #[test]
    fn inline_code() {
        let html = "<html><body><p>Run <code>cargo build</code> now.</p></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("`cargo build`"));
    }

    #[test]
    fn code_blocks() {
        let html = "<html><body><pre><code>fn main() {\n    println!(\"hi\");\n}</code></pre></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("```"));
        assert!(md.contains("fn main()"));
    }

    #[test]
    fn unordered_list() {
        let html = "<html><body><ul><li>Alpha</li><li>Beta</li><li>Gamma</li></ul></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("- Alpha"));
        assert!(md.contains("- Beta"));
        assert!(md.contains("- Gamma"));
    }

    #[test]
    fn ordered_list() {
        let html = "<html><body><ol><li>First</li><li>Second</li><li>Third</li></ol></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("1. First"), "got: {md}");
        assert!(md.contains("2. Second"), "got: {md}");
        assert!(md.contains("3. Third"), "got: {md}");
    }

    #[test]
    fn strips_scripts_and_styles() {
        let html = r#"<html><body>
            <script>alert('xss')</script>
            <style>.red { color: red; }</style>
            <p>Visible content</p>
            <nav><a href="/">Home</a></nav>
        </body></html>"#;
        let md = html_to_markdown(html);
        assert!(md.contains("Visible content"));
        assert!(!md.contains("alert"));
        assert!(!md.contains(".red"));
        assert!(!md.contains("Home"));
    }

    #[test]
    fn strips_nav_footer_aside() {
        let html = r#"<html><body>
            <main><p>Main content</p></main>
            <footer>Copyright 2026</footer>
            <aside>Sidebar stuff</aside>
        </body></html>"#;
        let md = html_to_markdown(html);
        assert!(md.contains("Main content"));
        assert!(!md.contains("Copyright"));
        assert!(!md.contains("Sidebar"));
    }

    #[test]
    fn extract_metadata_full() {
        let html = r#"<html lang="en">
        <head>
            <title>Test Page</title>
            <meta name="description" content="A test page for metadata extraction">
            <meta property="og:title" content="OG Title">
            <meta property="og:description" content="OG Description">
            <meta property="og:image" content="https://example.com/img.png">
            <meta property="og:url" content="https://example.com/page">
        </head>
        <body><p>Hello</p></body>
        </html>"#;
        let doc = Html::parse_document(html);
        let meta = extract_metadata(&doc);
        assert_eq!(meta.title.as_deref(), Some("Test Page"));
        assert_eq!(
            meta.description.as_deref(),
            Some("A test page for metadata extraction")
        );
        assert_eq!(meta.language.as_deref(), Some("en"));
        assert_eq!(meta.og_title.as_deref(), Some("OG Title"));
        assert_eq!(meta.og_description.as_deref(), Some("OG Description"));
        assert_eq!(
            meta.og_image.as_deref(),
            Some("https://example.com/img.png")
        );
        assert_eq!(
            meta.og_url.as_deref(),
            Some("https://example.com/page")
        );
    }

    #[test]
    fn extract_metadata_minimal() {
        let html = "<html><head></head><body></body></html>";
        let doc = Html::parse_document(html);
        let meta = extract_metadata(&doc);
        assert!(meta.title.is_none());
        assert!(meta.description.is_none());
        assert!(meta.language.is_none());
    }

    #[test]
    fn blockquote() {
        let html = "<html><body><blockquote>Quoted text here</blockquote></body></html>";
        let md = html_to_markdown(html);
        assert!(md.contains("> Quoted text here"));
    }

    #[test]
    fn whitespace_collapse() {
        let html = "<html><body><p>  lots   of    spaces  </p></body></html>";
        let md = html_to_markdown(html);
        assert!(!md.contains("  lots"));
        assert!(md.contains("lots of spaces"));
    }

    #[test]
    fn complex_page() {
        let html = r#"<html lang="en">
        <head><title>Blog Post</title></head>
        <body>
            <nav><a href="/">Home</a></nav>
            <article>
                <h1>My Blog Post</h1>
                <p>This is the <strong>first</strong> paragraph with a <a href="/link">link</a>.</p>
                <h2>Code Example</h2>
                <pre><code>let x = 42;</code></pre>
                <ul>
                    <li>Point one</li>
                    <li>Point two</li>
                </ul>
            </article>
            <footer>Copyright</footer>
            <script>tracking();</script>
        </body>
        </html>"#;
        let md = html_to_markdown(html);
        assert!(md.contains("# My Blog Post"));
        assert!(md.contains("**first**"));
        assert!(md.contains("[link](/link)"));
        assert!(md.contains("## Code Example"));
        assert!(md.contains("```"));
        assert!(md.contains("let x = 42;"));
        assert!(md.contains("- Point one"));
        assert!(!md.contains("Home"));
        assert!(!md.contains("Copyright"));
        assert!(!md.contains("tracking"));
    }
}
