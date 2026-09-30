import re
from dataclasses import dataclass
from enum import Enum


class Tier(Enum):
    FAST = 1
    BROWSER = 2
    STEALTH = 3


@dataclass
class RouteDecision:
    tier: Tier
    reason: str


SPA_INDICATORS = [
    "react", "angular", "vue", "__next", "__nuxt",
    "window.__INITIAL_STATE__", "data-reactroot",
]

JUNK_PATTERNS = [
    r"/cart", r"/login", r"/signup", r"/register",
    r"/tags/", r"/page/\d+", r"/wp-admin",
]


def classify_url(url: str, headers: dict[str, str] | None = None) -> RouteDecision:
    for pattern in JUNK_PATTERNS:
        if re.search(pattern, url, re.IGNORECASE):
            return RouteDecision(tier=Tier.FAST, reason=f"junk pattern: {pattern}")

    return RouteDecision(tier=Tier.FAST, reason="default static")


def classify_content(html: str) -> RouteDecision:
    html_lower = html.lower()
    for indicator in SPA_INDICATORS:
        if indicator in html_lower:
            return RouteDecision(tier=Tier.BROWSER, reason=f"SPA indicator: {indicator}")

    return RouteDecision(tier=Tier.FAST, reason="no SPA indicators")
