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

			// Resolve IP(s) for the host using the provided context
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			// Validate resolved IPs against policy
			var safeIP net.IP
			for _, ip := range ips {
				if isRestrictedIP(ip, cfg) {
					if !cfg.AllowLocalhosts {
						return nil, fmt.Errorf("blocked: host %q resolves to restricted loopback IP %s", host, ip)
					}
					// If allowing localhosts, proceed (but check other restrictions if any)
				}
				// We pick the first valid IP we find to dial
				if safeIP == nil {
					safeIP = ip
				}
			}

			if safeIP == nil {
				return nil, fmt.Errorf("blocked: host %q resolves to no allowed IPs", host)
			}

			// Dial the specific safe IP to prevent DNS rebinding attacks (TOCTOU)
			d := net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}

			// Use JoinHostPort to format IPv6 addresses correctly if needed
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
	if !cfg.AllowLocalhosts {
		if ip.IsLoopback() {
			return true
		}
	}
	return false
}
