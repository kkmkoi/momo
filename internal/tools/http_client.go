package tools

import (
	"net/http"
	"time"
)

// newHTTPClient creates an HTTP client matching crush's configuration:
// cloned default transport, connection pooling, 30s timeout, proxy-aware.
func newHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}
