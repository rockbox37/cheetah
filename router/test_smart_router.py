from smart_router import (
    Tier,
    RouteDecision,
    classify_url,
    classify_response,
    should_skip_url,
)


class TestRouteDecision:
    def test_confidence_clamped_to_zero(self):
        d = RouteDecision(tier=Tier.FAST, reason="test", confidence=-0.5)
        assert d.confidence == 0.0

    def test_confidence_clamped_to_one(self):
        d = RouteDecision(tier=Tier.FAST, reason="test", confidence=1.5)
        assert d.confidence == 1.0

    def test_confidence_normal(self):
        d = RouteDecision(tier=Tier.BROWSER, reason="test", confidence=0.7)
        assert d.confidence == 0.7


class TestClassifyUrlStaticDocs:
    def test_readthedocs(self):
        r = classify_url("https://requests.readthedocs.io/en/latest/")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_github_io(self):
        r = classify_url("https://user.github.io/project/docs/")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_docs_subdomain(self):
        r = classify_url("https://docs.python.org/3/library/json.html")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_wiki_subdomain(self):
        r = classify_url("https://wiki.archlinux.org/title/Pacman")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_mdn(self):
        r = classify_url("https://developer.mozilla.org/en-US/docs/Web/JavaScript")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_wikipedia(self):
        r = classify_url("https://en.wikipedia.org/wiki/Python_(programming_language)")
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8


class TestClassifyUrlSPA:
    def test_app_path(self):
        r = classify_url("https://example.com/app/settings")
        assert r.tier == Tier.BROWSER

    def test_dashboard_path(self):
        r = classify_url("https://example.com/dashboard/analytics")
        assert r.tier == Tier.BROWSER

    def test_hash_router(self):
        r = classify_url("https://example.com/#!/users/profile")
        assert r.tier == Tier.BROWSER

    def test_hash_router_simple(self):
        r = classify_url("https://example.com/#/home")
        assert r.tier == Tier.BROWSER

    def test_portal_path(self):
        r = classify_url("https://example.com/portal/tickets")
        assert r.tier == Tier.BROWSER


class TestClassifyUrlProtectedDomains:
    def test_linkedin(self):
        r = classify_url("https://www.linkedin.com/in/someone")
        assert r.tier == Tier.STEALTH
        assert r.confidence >= 0.8

    def test_instagram(self):
        r = classify_url("https://instagram.com/user")
        assert r.tier == Tier.STEALTH

    def test_facebook(self):
        r = classify_url("https://www.facebook.com/page")
        assert r.tier == Tier.STEALTH

    def test_twitter(self):
        r = classify_url("https://twitter.com/user/status/123")
        assert r.tier == Tier.STEALTH

    def test_zillow(self):
        r = classify_url("https://www.zillow.com/homes/for_sale/")
        assert r.tier == Tier.STEALTH

    def test_amazon(self):
        r = classify_url("https://www.amazon.com/dp/B001234")
        assert r.tier == Tier.STEALTH

    def test_subdomain_match(self):
        r = classify_url("https://mobile.linkedin.com/in/someone")
        assert r.tier == Tier.STEALTH


class TestClassifyUrlDefault:
    def test_unknown_url_defaults_fast(self):
        r = classify_url("https://example.com/blog/some-article")
        assert r.tier == Tier.FAST
        assert r.confidence == 0.5

    def test_with_non_html_content_type_header(self):
        r = classify_url(
            "https://example.com/data.json",
            headers={"content-type": "application/json"},
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8


class TestClassifyUrlCaseInsensitiveHeaders:
    def test_uppercase_content_type(self):
        r = classify_url(
            "https://example.com/data.json",
            headers={"CONTENT-TYPE": "application/json"},
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8
        assert "non-HTML content-type" in r.reason

    def test_mixed_case_content_type(self):
        r = classify_url(
            "https://example.com/feed.xml",
            headers={"Content-Type": "application/xml"},
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_title_case_content_type(self):
        r = classify_url(
            "https://example.com/api",
            headers={"Content-type": "application/json; charset=utf-8"},
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.8

    def test_html_content_type_any_case_not_fast(self):
        r = classify_url(
            "https://example.com/page",
            headers={"CONTENT-TYPE": "text/html; charset=utf-8"},
        )
        # HTML content-type should NOT trigger the non-HTML early return
        assert r.tier == Tier.FAST
        assert r.reason == "default"


class TestClassifyResponseNonHTML:
    def test_json_content_type(self):
        r = classify_response(
            url="https://api.example.com/data",
            status_code=200,
            headers={"content-type": "application/json"},
            body_preview='{"key": "value"}',
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.9

    def test_xml_content_type(self):
        r = classify_response(
            url="https://example.com/feed.xml",
            status_code=200,
            headers={"content-type": "application/xml"},
            body_preview="<rss><channel></channel></rss>",
        )
        assert r.tier == Tier.FAST


class TestClassifyResponseCaseInsensitiveHeaders:
    def test_uppercase_content_type(self):
        r = classify_response(
            url="https://api.example.com/data",
            status_code=200,
            headers={"CONTENT-TYPE": "application/json"},
            body_preview='{"key": "value"}',
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.9
        assert "non-HTML content-type" in r.reason

    def test_mixed_case_content_type(self):
        r = classify_response(
            url="https://example.com/feed.xml",
            status_code=200,
            headers={"Content-Type": "application/xml"},
            body_preview="<rss><channel></channel></rss>",
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.9

    def test_uppercase_challenge_header(self):
        r = classify_response(
            url="https://example.com",
            status_code=403,
            headers={
                "CONTENT-TYPE": "text/html",
                "CF-MITIGATED": "challenge",
            },
            body_preview="<html>blocked</html>",
        )
        assert r.tier == Tier.STEALTH
        assert "challenge header" in r.reason

    def test_mixed_case_datadome_header(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={
                "Content-Type": "text/html",
                "X-DataDome": "some-value",
            },
            body_preview="<html>page</html>",
        )
        assert r.tier == Tier.STEALTH

    def test_html_content_type_uppercase_falls_through(self):
        """HTML content-type in any case should not trigger non-HTML early return."""
        paragraphs = "\n".join(
            f"<p>Paragraph {i} with real content.</p>" for i in range(50)
        )
        body = f"<html><body>{paragraphs}</body></html>"
        r = classify_response(
            url="https://example.com/article",
            status_code=200,
            headers={"CONTENT-TYPE": "text/html; charset=utf-8"},
            body_preview=body,
        )
        assert r.tier == Tier.FAST
        assert r.reason == "large static HTML with content"


class TestClassifyResponseChallenges:
    def test_cloudflare_header(self):
        r = classify_response(
            url="https://example.com",
            status_code=403,
            headers={
                "content-type": "text/html",
                "cf-mitigated": "challenge",
            },
            body_preview="<html>blocked</html>",
        )
        assert r.tier == Tier.STEALTH
        assert "challenge header" in r.reason

    def test_datadome_header(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={
                "content-type": "text/html",
                "x-datadome": "some-value",
            },
            body_preview="<html>page</html>",
        )
        assert r.tier == Tier.STEALTH

    def test_cloudflare_body_on_403(self):
        r = classify_response(
            url="https://example.com",
            status_code=403,
            headers={"content-type": "text/html"},
            body_preview='<html><body>Please wait... Checking your browser before accessing</body></html>',
        )
        assert r.tier == Tier.STEALTH

    def test_challenge_body_just_a_moment(self):
        r = classify_response(
            url="https://example.com",
            status_code=503,
            headers={"content-type": "text/html"},
            body_preview="<html><title>Just a moment...</title></html>",
        )
        assert r.tier == Tier.STEALTH

    def test_challenge_body_on_200(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><body>Managed by Cloudflare cf_chl_opt</body></html>',
        )
        assert r.tier == Tier.STEALTH


class TestClassifyResponseSPA:
    def test_react_root(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><body><div id="root" data-reactroot=""></div><script src="/app.js"></script></body></html>',
        )
        assert r.tier == Tier.BROWSER

    def test_next_js(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><body><div id="__next"></div><script src="/_next/static/chunks/main.js"></script></body></html>',
        )
        assert r.tier == Tier.BROWSER

    def test_vue_app(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><body><div id="app" __vue_app__></div></body></html>',
        )
        assert r.tier == Tier.BROWSER

    def test_angular_app(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><body><app-root ng-version="15.0.0"></app-root></body></html>',
        )
        assert r.tier == Tier.BROWSER

    def test_small_body_with_spa_indicator(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<div id="root"></div><script src="/bundle.js"></script>',
        )
        assert r.tier == Tier.BROWSER
        assert r.confidence >= 0.8

    def test_small_body_script_heavy(self):
        r = classify_response(
            url="https://example.com",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview='<html><head><script src="a.js"></script><script src="b.js"></script></head><body></body></html>',
        )
        assert r.tier == Tier.BROWSER


class TestClassifyResponseStaticHTML:
    def test_large_static_html(self):
        paragraphs = "\n".join(f"<p>This is paragraph {i} with real content about various topics.</p>" for i in range(50))
        body = f"<html><body>{paragraphs}</body></html>"
        r = classify_response(
            url="https://example.com/article",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview=body,
        )
        assert r.tier == Tier.FAST
        assert r.confidence >= 0.7


class TestShouldSkipUrl:
    def test_cart(self):
        skip, reason = should_skip_url("https://example.com/cart")
        assert skip is True
        assert "cart" in reason.lower()

    def test_login(self):
        skip, reason = should_skip_url("https://example.com/login")
        assert skip is True

    def test_signup(self):
        skip, reason = should_skip_url("https://example.com/signup")
        assert skip is True

    def test_checkout(self):
        skip, reason = should_skip_url("https://example.com/checkout/step1")
        assert skip is True

    def test_wp_admin(self):
        skip, reason = should_skip_url("https://example.com/wp-admin/plugins.php")
        assert skip is True

    def test_feed(self):
        skip, reason = should_skip_url("https://example.com/feed/rss2")
        assert skip is True

    def test_rss(self):
        skip, reason = should_skip_url("https://example.com/rss")
        assert skip is True

    def test_print(self):
        skip, reason = should_skip_url("https://example.com/article/print")
        assert skip is True

    def test_share(self):
        skip, reason = should_skip_url("https://example.com/share/abc123")
        assert skip is True

    def test_account(self):
        skip, reason = should_skip_url("https://example.com/account/settings")
        assert skip is True

    def test_normal_url_not_skipped(self):
        skip, reason = should_skip_url("https://example.com/blog/great-article")
        assert skip is False
        assert reason == ""

    def test_docs_not_skipped(self):
        skip, reason = should_skip_url("https://docs.python.org/3/library/json.html")
        assert skip is False


class TestShouldSkipFileExtensions:
    def test_pdf(self):
        skip, _ = should_skip_url("https://example.com/report.pdf")
        assert skip is True

    def test_zip(self):
        skip, _ = should_skip_url("https://example.com/archive.zip")
        assert skip is True

    def test_exe(self):
        skip, _ = should_skip_url("https://example.com/setup.exe")
        assert skip is True

    def test_png(self):
        skip, _ = should_skip_url("https://example.com/image.png")
        assert skip is True

    def test_jpg(self):
        skip, _ = should_skip_url("https://example.com/photo.jpg")
        assert skip is True

    def test_mp4(self):
        skip, _ = should_skip_url("https://example.com/video.mp4")
        assert skip is True

    def test_css(self):
        skip, _ = should_skip_url("https://example.com/style.css")
        assert skip is True

    def test_js(self):
        skip, _ = should_skip_url("https://example.com/bundle.js")
        assert skip is True

    def test_html_not_skipped(self):
        skip, _ = should_skip_url("https://example.com/page.html")
        assert skip is False


class TestShouldSkipExcessiveQueryParams:
    def test_many_params_skipped(self):
        skip, reason = should_skip_url(
            "https://example.com/search?a=1&b=2&c=3&d=4&e=5&f=6"
        )
        assert skip is True
        assert "query parameters" in reason

    def test_few_params_ok(self):
        skip, _ = should_skip_url("https://example.com/search?q=python&page=1")
        assert skip is False

    def test_five_params_ok(self):
        skip, _ = should_skip_url(
            "https://example.com/search?a=1&b=2&c=3&d=4&e=5"
        )
        assert skip is False


class TestConfidenceScores:
    def test_protected_domain_high_confidence(self):
        r = classify_url("https://www.linkedin.com/in/someone")
        assert r.confidence >= 0.8

    def test_static_doc_high_confidence(self):
        r = classify_url("https://requests.readthedocs.io/en/latest/")
        assert r.confidence >= 0.8

    def test_spa_url_medium_confidence(self):
        r = classify_url("https://example.com/dashboard/home")
        assert 0.5 <= r.confidence <= 0.9

    def test_default_url_medium_confidence(self):
        r = classify_url("https://example.com/blog/post")
        assert r.confidence == 0.5

    def test_challenge_response_high_confidence(self):
        r = classify_response(
            url="https://example.com",
            status_code=403,
            headers={"content-type": "text/html", "cf-mitigated": "challenge"},
            body_preview="blocked",
        )
        assert r.confidence >= 0.8

    def test_static_response_high_confidence(self):
        long_body = "<html><body>" + "Real text content. " * 100 + "</body></html>"
        r = classify_response(
            url="https://example.com/article",
            status_code=200,
            headers={"content-type": "text/html"},
            body_preview=long_body,
        )
        assert r.confidence >= 0.7
