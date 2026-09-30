package main

import (
	"fmt"
	"net"
	"net/url"
)

// ValidateScrapeURL validates that rawURL is safe for server-side fetching.
// It rejects non-http(s) schemes and URLs that resolve to private/reserved IPs.
func ValidateScrapeURL(rawURL string) error {
	u, err := url.ParseRequestURI(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme must be http or https")
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url must include a hostname")
	}

	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("cannot resolve hostname: %s", host)
	}

	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if isPrivateIP(ip) {
			return fmt.Errorf("url resolves to a private or reserved IP address")
		}
	}

	return nil
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
