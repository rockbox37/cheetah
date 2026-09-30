import re
from dataclasses import dataclass
from enum import Enum
from urllib.parse import urlparse, parse_qs


class Tier(Enum):
    FAST = 1
    BROWSER = 2
    STEALTH = 3


@dataclass
class RouteDecision:
    tier: Tier
    reason: str
    confidence: float

    def __post_init__(self):
        self.confidence = max(0.0, min(1.0, self.confidence))


STATIC_DOC_HOSTS = [
    r"\.readthedocs\.io$",
    r"\.github\.io$",
    r"^docs\.",
    r"^wiki\.",
    r"^man\.",
    r"^manpages\.",
    r"\.rtfd\.io$",
    r"^javadoc\.",
    r"^apidocs?\.",
    r"\.gitbook\.io$",
    r"^en\.wikipedia\.org$",
    r"^developer\.mozilla\.org$",
]

SPA_URL_PATTERNS = [
    r"/app(/|$)",
    r"/dashboard(/|$)",
    r"/portal(/|$)",
    r"/console(/|$)",
    r"/workspace(/|$)",
]

HASH_ROUTER_PATTERNS = [
    r"/#/",
    r"/#!/",
]

PROTECTED_DOMAINS = [
    "linkedin.com",
    "instagram.com",
    "facebook.com",
    "twitter.com",
    "x.com",
    "tiktok.com",
    "zillow.com",
    "glassdoor.com",
    "indeed.com",
    "yelp.com",
    "amazon.com",
    "airbnb.com",
    "booking.com",
    "pinterest.com",
]

SKIP_PATH_PATTERNS = [
    (r"/cart(/|$|\?)", "shopping cart"),
    (r"/login(/|$|\?)", "login page"),
    (r"/signin(/|$|\?)", "sign-in page"),
    (r"/signup(/|$|\?)", "signup page"),
    (r"/register(/|$|\?)", "registration page"),
    (r"/account(/|$|\?)", "account page"),
    (r"/checkout(/|$|\?)", "checkout page"),
    (r"/wp-admin(/|$|\?)", "WordPress admin"),
    (r"/feed(/|$|\?)", "feed endpoint"),
    (r"/rss(/|$|\?)", "RSS feed"),
    (r"/print(/|$|\?)", "print view"),
    (r"/share(/|$|\?)", "share page"),
    (r"/unsubscribe(/|$|\?)", "unsubscribe page"),
    (r"/password[-_]?reset(/|$|\?)", "password reset"),
]

SKIP_EXTENSIONS = {
    ".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz", ".rar", ".7z",
    ".exe", ".msi", ".dmg", ".pkg", ".deb", ".rpm",
    ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".bmp", ".webp",
    ".mp3", ".mp4", ".avi", ".mov", ".mkv", ".wav", ".flac",
    ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
    ".iso", ".img", ".bin", ".apk", ".ipa",
    ".woff", ".woff2", ".ttf", ".eot",
    ".css", ".js", ".map",
}

SPA_BODY_INDICATORS = [
    "data-reactroot",
    "data-react-helmet",
    "__next_data__",
    "__next",
    "__nuxt",
    "__vue_app__",
    "ng-app",
    "ng-version",
    "ember-view",
    "data-turbo",
    "window.__initial_state__",
    "window.__preloaded_state__",
    'id="__next"',
    'id="app"',
    'id="root"',
]

CHALLENGE_HEADERS = [
    "cf-mitigated",
    "cf-chl-bypass",
    "x-datadome",
    "x-datadome-cid",
    "x-px-",
    "x-distil-cs",
]

CHALLENGE_BODY_SIGNATURES = [
    "managed by cloudflare",
    "challenge-platform",
    "cf-browser-verification",
    "cf_chl_opt",
    "datadome",
    "perimeterx",
    "distil_r_captcha",
    "just a moment",
    "checking your browser",
    "please verify you are a human",
    "access denied",
]


def classify_url(url: str, headers: dict[str, str] | None = None) -> RouteDecision:
    try:
        parsed = urlparse(url)
    except Exception:
        return RouteDecision(tier=Tier.FAST, reason="unparseable URL, default fast", confidence=0.3)

    hostname = (parsed.hostname or "").lower()

    for domain in PROTECTED_DOMAINS:
        if hostname == domain or hostname.endswith("." + domain):
            return RouteDecision(
                tier=Tier.STEALTH,
                reason=f"protected domain: {domain}",
                confidence=0.9,
            )

    for pattern in STATIC_DOC_HOSTS:
        if re.search(pattern, hostname):
            return RouteDecision(
                tier=Tier.FAST,
                reason=f"known static doc host: {hostname}",
                confidence=0.9,
            )

    path = parsed.path.lower()
    for pattern in SPA_URL_PATTERNS:
        if re.search(pattern, path):
            return RouteDecision(
                tier=Tier.BROWSER,
                reason=f"SPA URL pattern: {pattern}",
                confidence=0.7,
            )

    # Hash-based routing lives in the fragment, which urlparse strips from the path.
    # Check against the raw URL instead.
    for pattern in HASH_ROUTER_PATTERNS:
        if pattern in url:
            return RouteDecision(
                tier=Tier.BROWSER,
                reason=f"hash-based SPA router: {pattern}",
                confidence=0.8,
            )

    if headers:
        ct = headers.get("content-type", headers.get("Content-Type", ""))
        if ct and "text/html" not in ct and "application/xhtml" not in ct:
            return RouteDecision(
                tier=Tier.FAST,
                reason=f"non-HTML content-type: {ct}",
                confidence=0.9,
            )

    return RouteDecision(tier=Tier.FAST, reason="default", confidence=0.5)


def classify_response(
    url: str,
    status_code: int,
    headers: dict[str, str],
    body_preview: str,
) -> RouteDecision:
    ct = headers.get("content-type", headers.get("Content-Type", ""))
    if ct and "text/html" not in ct and "application/xhtml" not in ct:
        return RouteDecision(
            tier=Tier.FAST,
            reason=f"non-HTML content-type: {ct}",
            confidence=0.95,
        )

    headers_lower = {k.lower(): v.lower() for k, v in headers.items()}
    for challenge_hdr in CHALLENGE_HEADERS:
        for hdr_key in headers_lower:
            if hdr_key.startswith(challenge_hdr):
                return RouteDecision(
                    tier=Tier.STEALTH,
                    reason=f"challenge header detected: {hdr_key}",
                    confidence=0.9,
                )

    if status_code == 403 or status_code == 503:
        body_lower = body_preview.lower()
        for sig in CHALLENGE_BODY_SIGNATURES:
            if sig in body_lower:
                return RouteDecision(
                    tier=Tier.STEALTH,
                    reason=f"challenge signature in {status_code} response: {sig}",
                    confidence=0.9,
                )

    body_lower = body_preview.lower()

    for sig in CHALLENGE_BODY_SIGNATURES:
        if sig in body_lower:
            challenge_score = body_lower.count(sig)
            if challenge_score >= 1 and len(body_preview) < 50_000:
                return RouteDecision(
                    tier=Tier.STEALTH,
                    reason=f"challenge signature in body: {sig}",
                    confidence=0.8,
                )

    if len(body_preview.strip()) < 500:
        for indicator in SPA_BODY_INDICATORS:
            if indicator in body_lower:
                return RouteDecision(
                    tier=Tier.BROWSER,
                    reason=f"small body with SPA indicator: {indicator}",
                    confidence=0.85,
                )
        if "<script" in body_lower and body_lower.count("<script") >= 2:
            return RouteDecision(
                tier=Tier.BROWSER,
                reason="small body dominated by scripts",
                confidence=0.7,
            )

    for indicator in SPA_BODY_INDICATORS:
        if indicator in body_lower:
            return RouteDecision(
                tier=Tier.BROWSER,
                reason=f"SPA indicator in body: {indicator}",
                confidence=0.75,
            )

    text_content = re.sub(r"<[^>]+>", "", body_preview).strip()
    if len(text_content) > 500:
        return RouteDecision(
            tier=Tier.FAST,
            reason="large static HTML with content",
            confidence=0.8,
        )

    return RouteDecision(tier=Tier.FAST, reason="default static response", confidence=0.5)


def should_skip_url(url: str) -> tuple[bool, str]:
    try:
        parsed = urlparse(url)
    except Exception:
        return False, ""

    path = parsed.path.lower()

    for ext in SKIP_EXTENSIONS:
        if path.endswith(ext):
            return True, f"binary/non-HTML file extension: {ext}"

    for pattern, reason in SKIP_PATH_PATTERNS:
        if re.search(pattern, path, re.IGNORECASE):
            return True, f"junk URL pattern: {reason}"

    query_params = parse_qs(parsed.query)
    if len(query_params) > 5:
        return True, f"excessive query parameters: {len(query_params)}"

    return False, ""
