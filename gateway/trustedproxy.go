package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	headerProxyOwner     = "X-Cheetah-Owner"
	headerProxyPlan      = "X-Cheetah-Plan"
	headerProxyTimestamp = "X-Cheetah-Timestamp"
	headerProxySignature = "X-Cheetah-Signature"

	trustedProxyMaxSkew = 30 * time.Second

	// minProxySecretLen is the shortest ENGINE_PROXY_SECRET accepted at startup.
	minProxySecretLen = 32

	ownerHashLen = 64 // hex-encoded SHA-256
)

// SignProxyPlan returns the hex HMAC-SHA256 over the owner hash, the
// base64-encoded plan JSON, the unix timestamp, and the request method and
// URI, so a captured header set cannot be replayed against another endpoint.
// Mirrored in cheetah-cloud billing/proxysign.go; both are pinned by the same
// golden vector in their tests.
func SignProxyPlan(secret, owner, planB64, timestamp, method, uri string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(owner + "\n" + planB64 + "\n" + timestamp + "\n" + method + "\n" + uri))
	return hex.EncodeToString(mac.Sum(nil))
}

// isOwnerHash reports whether s is a lowercase hex SHA-256, the exact form
// hashAPIKey produces, so a differently-cased owner cannot form a second namespace.
func isOwnerHash(s string) bool {
	if len(s) != ownerHashLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

var (
	proxyRejectLastLog    atomic.Int64 // unix nanos of the last emitted line
	proxyRejectSuppressed atomic.Int64
)

const proxyRejectLogInterval = 10 * time.Second

// ignoreProxyPlan logs why presented proxy headers were not trusted, then lets
// the request continue unauthenticated-as-proxy (normal plan loader). The log
// is rate limited because any client can trigger it with one junk header
// before the rate limiter runs.
func ignoreProxyPlan(c *fiber.Ctx, reason string) error {
	now := time.Now().UnixNano()
	last := proxyRejectLastLog.Load()
	if now-last >= int64(proxyRejectLogInterval) && proxyRejectLastLog.CompareAndSwap(last, now) {
		log.Printf("trusted-proxy: ignored reason=%s ip=%s suppressed=%d",
			reason, anonymizeIP(c.IP()), proxyRejectSuppressed.Swap(0))
	} else {
		proxyRejectSuppressed.Add(1)
	}
	return c.Next()
}

// TrustedProxyPlan lets a trusted upstream (the cloud billing proxy) tell the
// engine who the caller is and which plan applies. It is a no-op when secret
// is empty, and any request whose signature, timestamp, owner, or plan fails
// validation is left untouched, so it falls through to the normal plan loader.
func TrustedProxyPlan(secret string) fiber.Handler {
	if secret == "" {
		return func(c *fiber.Ctx) error { return c.Next() }
	}
	return func(c *fiber.Ctx) error {
		owner := c.Get(headerProxyOwner)
		planB64 := c.Get(headerProxyPlan)
		ts := c.Get(headerProxyTimestamp)
		sig := c.Get(headerProxySignature)
		if owner == "" && planB64 == "" && ts == "" && sig == "" {
			return c.Next() // not proxied; the normal case for direct callers
		}

		want := SignProxyPlan(secret, owner, planB64, ts, c.Method(), c.OriginalURL())
		if !hmac.Equal([]byte(want), []byte(sig)) {
			return ignoreProxyPlan(c, "bad_signature")
		}
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return ignoreProxyPlan(c, "bad_timestamp")
		}
		skew := time.Since(time.Unix(sec, 0))
		if skew > trustedProxyMaxSkew || skew < -trustedProxyMaxSkew {
			return ignoreProxyPlan(c, "stale_timestamp")
		}
		if !isOwnerHash(owner) {
			return ignoreProxyPlan(c, "bad_owner")
		}
		raw, err := base64.StdEncoding.DecodeString(planB64)
		if err != nil {
			return ignoreProxyPlan(c, "bad_plan_encoding")
		}
		var plan Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return ignoreProxyPlan(c, "bad_plan_json")
		}

		c.Locals("owner_hash", owner)
		c.Locals("plan", plan)
		c.Locals("plan_trusted", true)
		return c.Next()
	}
}
