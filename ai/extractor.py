"""Structured extraction via local SLM (vLLM)."""


async def extract_json(html: str, schema: dict) -> dict:
    raise NotImplementedError("vLLM extraction not yet wired")


def html_to_markdown(html: str) -> str:
    raise NotImplementedError("DOM-to-Markdown not yet wired")


def auto_chunk(markdown: str, max_tokens: int = 512) -> list[dict]:
    raise NotImplementedError("Semantic chunking not yet wired")
