package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"time"
)

const testProxySecret = "proxy-secret-proxy-secret-proxy-secret"

var testOwner = strings.Repeat("ab", 32)

func trustedExtractRequest(t *testing.T, secret string, plan Plan, mutate func(*http.Request)) *http.Request {
	t.Helper()
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	planB64 := base64.StdEncoding.EncodeToString(raw)
	ts := strconv.FormatInt(time.Now().Unix(), 10)

	// No extract_schema: a request that passes the can_extract gate is
	// rejected with 400 afterwards, so 403 vs 400 distinguishes the plans.
	body := bytes.NewBufferString(`{"url": "https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/extract", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(headerProxyOwner, testOwner)
	req.Header.Set(headerProxyPlan, planB64)
	req.Header.Set(headerProxyTimestamp, ts)
	req.Header.Set(headerProxySignature, SignProxyPlan(secret, testOwner, planB64, ts, http.MethodPost, "/v1/extract"))
	if mutate != nil {
		mutate(req)
	}
	return req
}

func trustedProxyApp(secret string) *fiber.App {
	cfg := Config{
		RedisURL:      "localhost:6379",
		Port:          "3000",
		AIEndpointURL: "https://ai.example.com/v1",
		ProxySecret:   secret,
	}
	return NewApp(cfg, NewQueueClient(cfg.RedisURL))
}

func TestTrustedProxyPlanAppliesPlan(t *testing.T) {
	app := trustedProxyApp(testProxySecret)
	pro := Plan{Name: "pro", CanExtract: true, MaxRatePerMinute: 100}

	resp, err := app.Test(trustedExtractRequest(t, testProxySecret, pro, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (past can_extract gate), got %d", resp.StatusCode)
	}
}

func TestTrustedProxyPlanRejectsBadInput(t *testing.T) {
	app := trustedProxyApp(testProxySecret)
	pro := Plan{Name: "pro", CanExtract: true, MaxRatePerMinute: 100}

	cases := map[string]func(*http.Request){
		"bad signature": func(r *http.Request) { r.Header.Set(headerProxySignature, strings.Repeat("0", 64)) },
		"tampered plan": func(r *http.Request) {
			forged, _ := json.Marshal(Plan{Name: "scale", CanExtract: true, MaxRatePerMinute: 9999})
			r.Header.Set(headerProxyPlan, base64.StdEncoding.EncodeToString(forged))
		},
		"stale timestamp": func(r *http.Request) {
			ts := strconv.FormatInt(time.Now().Add(-5*time.Minute).Unix(), 10)
			planB64 := r.Header.Get(headerProxyPlan)
			r.Header.Set(headerProxyTimestamp, ts)
			r.Header.Set(headerProxySignature, SignProxyPlan(testProxySecret, testOwner, planB64, ts, http.MethodPost, "/v1/extract"))
		},
		"replayed on another URI": func(r *http.Request) {
			r.URL.RawQuery = "x=1"
			r.RequestURI = "/v1/extract?x=1"
		},
		"uppercase owner": func(r *http.Request) {
			up := strings.ToUpper(testOwner)
			planB64 := r.Header.Get(headerProxyPlan)
			ts := r.Header.Get(headerProxyTimestamp)
			r.Header.Set(headerProxyOwner, up)
			r.Header.Set(headerProxySignature, SignProxyPlan(testProxySecret, up, planB64, ts, http.MethodPost, "/v1/extract"))
		},
		"missing signature": func(r *http.Request) { r.Header.Del(headerProxySignature) },
		"non-hash owner": func(r *http.Request) {
			planB64 := r.Header.Get(headerProxyPlan)
			ts := r.Header.Get(headerProxyTimestamp)
			r.Header.Set(headerProxyOwner, "anonymous")
			r.Header.Set(headerProxySignature, SignProxyPlan(testProxySecret, "anonymous", planB64, ts, http.MethodPost, "/v1/extract"))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			resp, err := app.Test(trustedExtractRequest(t, testProxySecret, pro, mutate))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("expected 403 (fell back to free plan), got %d", resp.StatusCode)
			}
		})
	}
}

func TestTrustedProxyPlanIgnoredWithoutSecret(t *testing.T) {
	app := trustedProxyApp("")
	pro := Plan{Name: "pro", CanExtract: true, MaxRatePerMinute: 100}

	// Signed with a secret the engine does not have configured.
	resp, err := app.Test(trustedExtractRequest(t, testProxySecret, pro, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 when engine has no proxy secret, got %d", resp.StatusCode)
	}
}

func TestSignProxyPlanGoldenVector(t *testing.T) {
	// Same vector as cheetah-cloud billing/proxysign_test.go: a mismatch means
	// the two repos' signing has drifted apart.
	got := SignProxyPlan("s", "o", "p", "1", "POST", "/v1/scrape?x=1")
	const want = "3a041c91b77cde97262a3e9ece74ea0a44d81ebcdd03ea26ce230c015253b273"
	if got != want {
		t.Fatalf("golden vector = %s", got)
	}
}
