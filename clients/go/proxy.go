package proxyflare

import (
	"errors"
	"net/url"
	"sync/atomic"
	"time"
)

var ErrNoAvailableProxies = errors.New("proxyflare: no available proxies")

// Proxy represents a proxy worker with thread-safe operations.
type Proxy struct {
	address        *url.URL
	availableAfter atomic.Int64
}

// NewProxy creates a new Proxy instance for a given worker URL.
func NewProxy(address *url.URL) *Proxy {
	return &Proxy{
		address: address,
	}
}

// SetAvailableAfter sets the proxy available status after a specific Unix timestamp.
func (p *Proxy) SetAvailableAfter(timestamp int64) {
	p.availableAfter.Store(timestamp)
}

// BanFor marks the proxy as unavailable for the specified duration.
func (p *Proxy) BanFor(d time.Duration) {
	p.SetAvailableAfter(time.Now().Add(d).Unix())
}

// IsAvailable checks whether the proxy is available at the provided Unix timestamp.
func (p *Proxy) IsAvailable(now time.Time) bool {
	return p.availableAfter.Load() < now.Unix()
}

// Address returns the proxy worker base URL.
func (p *Proxy) Address() url.URL {
	return *p.address
}
