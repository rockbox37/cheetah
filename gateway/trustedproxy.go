package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
)

const (
	headerProxyOwner     = "X-Cheetah-Owner"
	headerProxyPlan      = "X-Cheetah-Plan"
	headerProxyTimestamp = "X-Cheetah-Timestamp"
	headerProxySignature = "X-Cheetah-Signature"

	trustedProxyMaxSkew = 60 * time.Second
)

// SignProxyPlan returns the hex HMAC-SHA256 over the owner hash, the
// base64-encoded plan JSON, and the unix timestamp. Exported so the cloud
// wrapper that fronts the engine can produce the same signature.
func SignProxyPlan(secret, owner, planB64, timestamp string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(owner + "\n" + planB64 + "\n" + timestamp))
	return hex.EncodeToString(mac.Sum(nil))
}

func isOwnerHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
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
		if owner == "" || planB64 == "" || ts == "" || sig == "" {
			return c.Next()
		}

		want := SignProxyPlan(secret, owner, planB64, ts)
		if !hmac.Equal([]byte(want), []byte(sig)) {
			return c.Next()
		}
		sec, err := strconv.ParseInt(ts, 10, 64)
		if err != nil {
			return c.Next()
		}
		skew := time.Since(time.Unix(sec, 0))
		if skew > trustedProxyMaxSkew || skew < -trustedProxyMaxSkew {
			return c.Next()
		}
		if !isOwnerHash(owner) {
			return c.Next()
		}
		raw, err := base64.StdEncoding.DecodeString(planB64)
		if err != nil {
			return c.Next()
		}
		var plan Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return c.Next()
		}

		c.Locals("owner_hash", owner)
		c.Locals("plan", plan)
		c.Locals("plan_trusted", true)
		return c.Next()
	}
}
