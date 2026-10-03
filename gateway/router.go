package main

import (
	"log"
	"net/url"
	"regexp"
	"strings"
)

type Tier string

const (
	TierFast    Tier = "fast"
	TierBrowser Tier = "browser"
	TierStealth Tier = "stealth"
)

type RouteDecision struct {
	Tier       Tier    `json:"tier"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
}

func StreamForTier(tier Tier) string {
	switch tier {
	case TierBrowser:
		return "browser_jobs"
	case TierStealth:
		return "stealth_jobs"
	default:
		return "scrape_jobs"
	}
}

var protectedDomains = []string{
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
}

var staticDocHostPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\.readthedocs\.io$`),
	regexp.MustCompile(`\.github\.io$`),
	regexp.MustCompile(`^docs\.`),
	regexp.MustCompile(`^wiki\.`),
	regexp.MustCompile(`^man\.`),
	regexp.MustCompile(`^manpages\.`),
	regexp.MustCompile(`\.rtfd\.io$`),
	regexp.MustCompile(`^javadoc\.`),
	regexp.MustCompile(`^apidocs?\.`),
	regexp.MustCompile(`\.gitbook\.io$`),
	regexp.MustCompile(`^en\.wikipedia\.org$`),
	regexp.MustCompile(`^developer\.mozilla\.org$`),
}

var spaPathPatterns = []*regexp.Regexp{
	regexp.MustCompile(`/app(/|$)`),
	regexp.MustCompile(`/dashboard(/|$)`),
	regexp.MustCompile(`/portal(/|$)`),
	regexp.MustCompile(`/console(/|$)`),
	regexp.MustCompile(`/workspace(/|$)`),
}

var hashRouterPatterns = []string{
	"/#/",
	"/#!/",
}

func ClassifyURL(rawURL string) RouteDecision {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return RouteDecision{Tier: TierFast, Reason: "unparseable URL", Confidence: 0.3}
	}

	hostname := strings.ToLower(parsed.Hostname())

	for _, domain := range protectedDomains {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return RouteDecision{
				Tier:       TierStealth,
				Reason:     "protected domain: " + domain,
				Confidence: 0.9,
			}
		}
	}

	for _, pat := range staticDocHostPatterns {
		if pat.MatchString(hostname) {
			return RouteDecision{
				Tier:       TierFast,
				Reason:     "known static doc host: " + hostname,
				Confidence: 0.9,
			}
		}
	}

	path := strings.ToLower(parsed.Path)
	for _, pat := range spaPathPatterns {
		if pat.MatchString(path) {
			return RouteDecision{
				Tier:       TierBrowser,
				Reason:     "SPA URL pattern: " + pat.String(),
				Confidence: 0.7,
			}
		}
	}

	for _, pattern := range hashRouterPatterns {
		if strings.Contains(rawURL, pattern) {
			return RouteDecision{
				Tier:       TierBrowser,
				Reason:     "hash-based SPA router: " + pattern,
				Confidence: 0.8,
			}
		}
	}

	return RouteDecision{Tier: TierFast, Reason: "default", Confidence: 0.5}
}

func IsTierAllowed(tier Tier, allowed []string) bool {
	if len(allowed) == 0 {
		return tier == TierFast
	}
	for _, a := range allowed {
		if Tier(a) == tier {
			return true
		}
	}
	return false
}

func LogRouteDecision(jobID string, rawURL string, decision RouteDecision) {
	redacted := rawURL
	if parsed, err := url.Parse(rawURL); err == nil {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		redacted = parsed.String()
	}
	log.Printf("route %s: tier=%s reason=%q confidence=%.2f url=%s",
		jobID, decision.Tier, decision.Reason, decision.Confidence, redacted)
}
