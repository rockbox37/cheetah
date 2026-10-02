package main

import (
	"fmt"
	"log"
	"net"
	"net/url"
)

// ValidateScrapeURL validates that rawURL is safe for server-side fetching.
// It rejects non-http(s) schemes and URLs that resolve to private/reserved IPs.
// On success it returns the first validated IP so callers can pin it in the job
// payload, closing the TOCTOU window between validation and worker fetch.
func ValidateScrapeURL(rawURL string) (string, error) {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		log.Printf("url validation failed: %v", err)
		return "", fmt.Errorf("invalid url")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("url scheme must be http or https")
	}

	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("url must include a hostname")
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("cannot resolve hostname: %s", host)
	}

	var pinnedIP string
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return "", fmt.Errorf("url resolves to a private or reserved IP address")
		}
		if pinnedIP == "" {
			pinnedIP = ipStr
		}
	}

	if pinnedIP == "" {
		return "", fmt.Errorf("cannot resolve hostname: %s", host)
	}

	return pinnedIP, nil
}

// isPrivateIP returns true if ip falls within any private, loopback,
// link-local, or otherwise reserved range that should not be reachable
// from server-side requests.
func isPrivateIP(ip net.IP) bool {
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}
