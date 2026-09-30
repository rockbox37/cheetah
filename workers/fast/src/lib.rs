use reqwest::Client;
use scraper::{Html, Selector};

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

#[cfg(test)]
mod tests {
    use super::*;
    use scraper::Html;

    #[test]
    fn parse_simple_html() {
        let html = "<html><body><p>Hello world</p></body></html>";
        let document = Html::parse_document(html);
        let selector = Selector::parse("body").unwrap();
        let text: String = document
            .select(&selector)
            .flat_map(|el| el.text())
            .collect::<Vec<_>>()
            .join("\n");
        assert!(text.contains("Hello world"));
    }
}
