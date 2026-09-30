import pytest
from extractor import extract_json, html_to_markdown, auto_chunk


def test_extract_json_not_implemented():
    with pytest.raises(NotImplementedError):
        import asyncio
        asyncio.run(extract_json("<p>test</p>", {}))


def test_html_to_markdown_not_implemented():
    with pytest.raises(NotImplementedError):
        html_to_markdown("<p>test</p>")


def test_auto_chunk_not_implemented():
    with pytest.raises(NotImplementedError):
        auto_chunk("# Hello")
