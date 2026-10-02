package main

import (
	"net"
	"testing"
)

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		private bool
	}{
		// Loopback
		{"loopback v4", "127.0.0.1", true},
		{"loopback v4 high", "127.255.255.255", true},
		{"loopback v6", "::1", true},

		// Private RFC1918
		{"private 10/8", "10.0.0.1", true},
		{"private 10/8 high", "10.255.255.255", true},
		{"private 172.16/12", "172.16.0.1", true},
		{"private 172.31/12", "172.31.255.255", true},
		{"private 192.168/16", "192.168.1.1", true},
		{"private 192.168/16 high", "192.168.255.255", true},

		// Link-local
		{"link-local v4", "169.254.1.1", true},
		{"metadata endpoint", "169.254.169.254", true},
		{"link-local v6", "fe80::1", true},

		// IPv6 unique local
		{"unique local fc00", "fc00::1", true},
		{"unique local fd00", "fd00::1", true},

		// Unspecified
		{"unspecified v4", "0.0.0.0", true},
		{"unspecified v6", "::", true},

		// Public IPs (should NOT be private)
		{"public 8.8.8.8", "8.8.8.8", false},
		{"public 1.1.1.1", "1.1.1.1", false},
		{"public 172.15.255.255", "172.15.255.255", false},
		{"public 172.32.0.0", "172.32.0.0", false},
		{"public 11.0.0.1", "11.0.0.1", false},
		{"public 192.169.0.1", "192.169.0.1", false},
		{"public v6", "2607:f8b0:4004:800::200e", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			if ip == nil {
				t.Fatalf("failed to parse IP: %s", tt.ip)
			}
			got := isPrivateIP(ip)
			if got != tt.private {
				t.Errorf("isPrivateIP(%s) = %v, want %v", tt.ip, got, tt.private)
			}
		})
	}
}

func TestValidateScrapeURL_Scheme(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https ok", "https://example.com", false},
		{"http ok", "http://example.com", false},
		{"ftp rejected", "ftp://example.com/file", true},
		{"file rejected", "file:///etc/passwd", true},
		{"javascript rejected", "javascript:alert(1)", true},
		{"data rejected", "data:text/html,<h1>hi</h1>", true},
		{"no scheme", "example.com", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateScrapeURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScrapeURL(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateScrapeURL_PrivateIPs(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"loopback literal", "http://127.0.0.1/", true},
		{"loopback v6 literal", "http://[::1]/", true},
		{"private 10.x", "http://10.0.0.1/", true},
		{"private 172.16.x", "http://172.16.0.1/", true},
		{"private 192.168.x", "http://192.168.1.1/", true},
		{"link-local", "http://169.254.169.254/latest/meta-data/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ValidateScrapeURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateScrapeURL(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateScrapeURL_ReturnsPinnedIP(t *testing.T) {
	ip, err := ValidateScrapeURL("https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip == "" {
		t.Fatal("expected a non-empty pinned IP")
	}
	parsed := net.ParseIP(ip)
	if parsed == nil {
		t.Fatalf("returned IP %q is not a valid IP address", ip)
	}
	if isPrivateIP(parsed) {
		t.Fatalf("returned IP %q is private", ip)
	}
}
