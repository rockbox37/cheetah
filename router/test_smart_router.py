from smart_router import Tier, classify_url, classify_content


def test_classify_url_default_static():
    result = classify_url("https://docs.example.com/getting-started")
    assert result.tier == Tier.FAST


def test_classify_url_junk_pattern():
    result = classify_url("https://example.com/cart")
    assert result.tier == Tier.FAST
    assert "junk" in result.reason


def test_classify_content_static():
    result = classify_content("<html><body><p>Hello</p></body></html>")
    assert result.tier == Tier.FAST


def test_classify_content_spa():
    result = classify_content('<html><body><div data-reactroot="">App</div></body></html>')
    assert result.tier == Tier.BROWSER
