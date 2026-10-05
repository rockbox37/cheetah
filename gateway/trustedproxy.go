package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	headerProxyOwner     = "X-Cheetah-Owner"
	headerProxyPlan      = "X-Cheetah-Plan"
	headerProxyTimestamp = "X-Cheetah-Timestamp"
	headerProxySignature = "X-Cheetah-Signature"

	trustedProxyMaxSkew = 30 * time.Second

	// MinProxySecretLen is the shortest ENGINE_PROXY_SECRET accepted at startup.
	MinProxySecretLen = 32

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

func rejectProxyPlan(c *fiber.Ctx, reason string) error {
	log.Printf("trusted-proxy: rejected reason=%s ip=%s", reason, anonymizeIP(c.IP()))
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
			return rejectProxyPlan(c, "bad_signature")
		}
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return rejectProxyPlan(c, "bad_timestamp")
		}
		skew := time.Since(time.Unix(sec, 0))
		if skew > trustedProxyMaxSkew || skew < -trustedProxyMaxSkew {
			return rejectProxyPlan(c, "stale_timestamp")
		}
		if !isOwnerHash(owner) {
			return rejectProxyPlan(c, "bad_owner")
		}
		raw, err := base64.StdEncoding.DecodeString(planB64)
		if err != nil {
			return rejectProxyPlan(c, "bad_plan_encoding")
		}
		var plan Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return rejectProxyPlan(c, "bad_plan_json")
		}

		c.Locals("owner_hash", owner)
		c.Locals("plan", plan)
		c.Locals("plan_trusted", true)
		return c.Next()
	}
}
