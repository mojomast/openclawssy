package tools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"openclawssy/internal/config"
)

func createSafeTransport(cfg config.NetworkConfig) *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			var safeIP net.IP
			for _, ip := range ips {
				if isRestrictedIP(ip, cfg) {
					return nil, fmt.Errorf("blocked: host %q resolves to restricted loopback/private IP %s", host, ip)
				}
				if safeIP == nil {
					safeIP = ip
				}
			}

			if safeIP == nil {
				return nil, fmt.Errorf("blocked: host %q resolves to no allowed IPs", host)
			}

			d := net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return d.DialContext(ctx, network, net.JoinHostPort(safeIP.String(), port))
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func isRestrictedIP(ip net.IP, cfg config.NetworkConfig) bool {
	if ip == nil {
		return true
	}
	if ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip.IsLoopback() {
		return !cfg.AllowLocalhosts
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return !cfg.AllowPrivateNetworks
	}
	return false
}
