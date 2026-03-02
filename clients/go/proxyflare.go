// Package proxyflare implements a client for ProxyFlare (Cloudflare Workers based) proxy distribution.
// Also implements a round-robin proxy provider, for proxy rotation.
// See https://github.com/defernest/proxyflare for more information.
package proxyflare

import (
	"net/http"
	"time"
)

// ProxyProvider defines an interface for providing proxies.
type ProxyProvider interface {
	// Next returns the next available proxy from the pool or ErrNoAvailableProxies.
	Next(now time.Time) (*Proxy, error)
}

// Transport implements the [http.RoundTripper] interface.
// It uses round-robin to distribute requests across available proxy workers.
// Target URL from `req.URL` is sent to proxy-workers by request header `X-Target-Url`.
type Transport struct {
	base     http.RoundTripper
	provider ProxyProvider
}

// NewTransport creates a Transport with round-robin proxies distribution.
// If base is nil, [http.DefaultTransport] is used.
func NewTransport(provider ProxyProvider, base http.RoundTripper) *Transport {
	if provider == nil {
		panic("proxyflare: provider cannot be nil")
	}
	if base == nil {
		base = http.DefaultTransport
	}
	t := &Transport{
		provider: provider,
		base:     base,
	}
	return t
}

// Base returns the base round-tripper.
func (pt *Transport) Base() http.RoundTripper {
	return pt.base
}

// Provider returns the proxy provider.
func (pt *Transport) Provider() ProxyProvider {
	return pt.provider
}

// RoundTrip executes a single HTTP transaction.
func (pt *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	selectedProxy, err := pt.provider.Next(time.Now())
	if err != nil {
		return nil, err
	}

	// Clone the request to avoid modifying the original one per http.RoundTripper contract
	clonedReq := req.Clone(req.Context())

	// Get the target URL
	target := clonedReq.URL.String()

	// Set the proxy URL and host
	proxyURL := selectedProxy.Address()
	clonedReq.URL = &proxyURL
	clonedReq.Host = proxyURL.Host

	// Set the target URL header
	if clonedReq.Header == nil {
		clonedReq.Header = make(http.Header)
	}
	clonedReq.Header.Set("X-Target-Url", target)

	// Execute the request
	return pt.base.RoundTrip(clonedReq)
}
