package tools

import (
	"net"
	"testing"

	"openclawssy/internal/config"
)

func TestIsRestrictedIP_DefaultPolicy(t *testing.T) {
	cfg := config.NetworkConfig{}
	tests := []struct {
		name       string
		ip         string
		restricted bool
	}{
		{name: "loopback v4", ip: "127.0.0.1", restricted: true},
		{name: "loopback v6", ip: "::1", restricted: true},
		{name: "private v4", ip: "10.0.0.5", restricted: true},
		{name: "private v6 ula", ip: "fd12:3456::1", restricted: true},
		{name: "link local", ip: "169.254.10.5", restricted: true},
		{name: "unspecified v4", ip: "0.0.0.0", restricted: true},
		{name: "unspecified v6", ip: "::", restricted: true},
		{name: "multicast v4", ip: "224.0.0.1", restricted: true},
		{name: "multicast v6", ip: "ff02::1", restricted: true},
		{name: "limited broadcast v4", ip: "255.255.255.255", restricted: true},
		{name: "public", ip: "8.8.8.8", restricted: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("failed to parse ip %q", tc.ip)
			}
			if got := isRestrictedIP(ip, cfg); got != tc.restricted {
				t.Fatalf("expected restricted=%t for %s, got %t", tc.restricted, tc.ip, got)
			}
		})
	}
}

func TestIsRestrictedIP_AllowOverrides(t *testing.T) {
	cfg := config.NetworkConfig{AllowLocalhosts: true, AllowPrivateNetworks: true}
	for _, raw := range []string{"127.0.0.1", "::1", "10.0.0.9", "fd12:3456::5", "169.254.10.5"} {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("failed to parse ip %q", raw)
		}
		if isRestrictedIP(ip, cfg) {
			t.Fatalf("expected %s to be allowed with explicit overrides", raw)
		}
	}
}

func TestIsRestrictedIP_AlwaysBlocksNonGlobalUnicast(t *testing.T) {
	cfg := config.NetworkConfig{AllowLocalhosts: true, AllowPrivateNetworks: true}
	for _, raw := range []string{"0.0.0.0", "::", "224.0.0.1", "ff02::1", "255.255.255.255"} {
		ip := net.ParseIP(raw)
		if ip == nil {
			t.Fatalf("failed to parse ip %q", raw)
		}
		if !isRestrictedIP(ip, cfg) {
			t.Fatalf("expected %s to remain restricted", raw)
		}
	}
}

func TestFilterAllowedIPs(t *testing.T) {
	cfg := config.NetworkConfig{}
	ips := []net.IP{
		net.ParseIP("10.0.0.5"),
		net.ParseIP("8.8.8.8"),
		net.ParseIP("127.0.0.1"),
		net.ParseIP("1.1.1.1"),
	}

	allowed, blocked := filterAllowedIPs(ips, cfg)
	if len(allowed) != 2 {
		t.Fatalf("expected 2 allowed IPs, got %d", len(allowed))
	}
	if len(blocked) != 2 {
		t.Fatalf("expected 2 blocked IPs, got %d", len(blocked))
	}
}

func TestCreateSafeTransportDisablesProxy(t *testing.T) {
	transport := createSafeTransport(config.NetworkConfig{})
	if transport.Proxy != nil {
		t.Fatal("expected transport proxy to be disabled for http.request")
	}
}
