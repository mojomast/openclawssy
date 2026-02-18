package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"openclawssy/internal/config"
)

const (
	defaultHTTPRequestTimeout = 15 * time.Second
	maxHTTPRedirects          = 10
	defaultHTTPResponseBytes  = 1 * 1024 * 1024
	maxHTTPResponseBytes      = 5 * 1024 * 1024
)

func registerNetworkTools(reg *Registry, configuredPath string) error {
	if err := reg.Register(ToolSpec{
		Name:        "http.request",
		Description: "Make allowlisted HTTP request",
		Required:    []string{"url"},
		ArgTypes: map[string]ArgType{
			"method":             ArgTypeString,
			"url":                ArgTypeString,
			"headers":            ArgTypeObject,
			"timeout_ms":         ArgTypeNumber,
			"max_response_bytes": ArgTypeNumber,
		},
	}, httpRequest(configuredPath)); err != nil {
		return err
	}
	return nil
}

type preparedHTTPRequest struct {
	Client   *http.Client
	Request  *http.Request
	MaxBytes int
}

func httpRequest(configuredPath string) Handler {
	return func(ctx context.Context, req Request) (map[string]any, error) {
		prep, err := prepareRequest(ctx, req, configuredPath)
		if err != nil {
			return nil, err
		}

		resp, err := executeRequest(prep.Client, prep.Request)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		return processResponse(resp, prep.MaxBytes)
	}
}

func prepareRequest(ctx context.Context, req Request, configuredPath string) (*preparedHTTPRequest, error) {
	cfgPath, err := resolveConfigPath(req.Workspace, configuredPath)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadOrDefault(cfgPath)
	if err != nil {
		return nil, err
	}
	if !cfg.Network.Enabled {
		return nil, errors.New("network is disabled (set network.enabled=true)")
	}

	urlText, err := getString(req.Args, "url")
	if err != nil {
		return nil, err
	}
	parsedURL, err := parseAndValidateNetworkURL(urlText, cfg.Network)
	if err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", req.Args["method"])))
	if method == "" || method == "<NIL>" {
		method = http.MethodGet
	}

	bodyBytes, err := httpRequestBody(req.Args)
	if err != nil {
		return nil, err
	}

	timeout := durationFromMS(req.Args["timeout_ms"], defaultHTTPRequestTimeout)
	if timeout <= 0 {
		timeout = defaultHTTPRequestTimeout
	}

	maxBytes := intFromAny(req.Args["max_response_bytes"], defaultHTTPResponseBytes)
	if maxBytes <= 0 {
		maxBytes = defaultHTTPResponseBytes
	}
	if maxBytes > maxHTTPResponseBytes {
		maxBytes = maxHTTPResponseBytes
	}

	httpClient := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(redirectReq *http.Request, via []*http.Request) error {
			if len(via) >= maxHTTPRedirects {
				return errors.New("stopped after too many redirects")
			}
			if _, err := parseAndValidateNetworkURL(redirectReq.URL.String(), cfg.Network); err != nil {
				return err
			}
			return nil
		},
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	for key, value := range parseRequestHeaders(req.Args["headers"]) {
		httpReq.Header.Set(key, value)
	}

	return &preparedHTTPRequest{
		Client:   httpClient,
		Request:  httpReq,
		MaxBytes: maxBytes,
	}, nil
}

func executeRequest(client *http.Client, req *http.Request) (*http.Response, error) {
	return client.Do(req)
}

func processResponse(resp *http.Response, maxBytes int) (map[string]any, error) {
	limited := io.LimitReader(resp.Body, int64(maxBytes+1))
	readBody, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(readBody) > maxBytes {
		truncated = true
		readBody = readBody[:maxBytes]
	}

	return map[string]any{
		"status":             resp.StatusCode,
		"headers":            flattenHeaders(resp.Header),
		"body":               string(readBody),
		"bytes_read":         len(readBody),
		"truncated":          truncated,
		"url":                resp.Request.URL.String(),
		"max_response_bytes": maxBytes,
	}, nil
}

func parseAndValidateNetworkURL(rawURL string, netCfg config.NetworkConfig) (*url.URL, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errors.New("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "http" && scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme: %q", parsed.Scheme)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return nil, errors.New("url host is required")
	}

	if isLocalhostHost(host) {
		if !netCfg.AllowLocalhosts {
			return nil, fmt.Errorf("host %q is localhost/loopback and network.allow_localhosts is false", host)
		}
		return parsed, nil
	}

	if !hostAllowedByDomainList(host, netCfg.AllowedDomains) {
		return nil, fmt.Errorf("host %q is not in network.allowed_domains", host)
	}

	return parsed, nil
}

func isLocalhostHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func hostAllowedByDomainList(host string, allowedDomains []string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, raw := range allowedDomains {
		candidate := strings.ToLower(strings.TrimSpace(raw))
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "*.") {
			candidate = strings.TrimPrefix(candidate, "*.")
		}
		if strings.HasPrefix(candidate, ".") {
			candidate = strings.TrimPrefix(candidate, ".")
		}
		if candidate == "" {
			continue
		}
		if host == candidate || strings.HasSuffix(host, "."+candidate) {
			return true
		}
	}
	return false
}

func parseRequestHeaders(raw any) map[string]string {
	headers := map[string]string{}
	obj, ok := raw.(map[string]any)
	if !ok {
		return headers
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" {
			continue
		}
		headers[cleanKey] = strings.TrimSpace(fmt.Sprintf("%v", obj[key]))
	}
	return headers
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for key, values := range h {
		if len(values) == 0 {
			continue
		}
		out[key] = strings.Join(values, ",")
	}
	return out
}

func httpRequestBody(args map[string]any) ([]byte, error) {
	raw, ok := args["body"]
	if !ok || raw == nil {
		return nil, nil
	}
	switch value := raw.(type) {
	case string:
		return []byte(value), nil
	case []byte:
		return value, nil
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("body must be string or JSON-serializable value: %w", err)
		}
		return encoded, nil
	}
}

func durationFromMS(raw any, fallback time.Duration) time.Duration {
	ms := intFromAny(raw, int(fallback/time.Millisecond))
	if ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func intFromAny(raw any, fallback int) int {
	if raw == nil {
		return fallback
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if s == "" || s == "<nil>" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
