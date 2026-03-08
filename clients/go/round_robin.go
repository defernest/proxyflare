package proxyflare

import (
	"errors"
	"sync/atomic"
	"time"
)

var (
	ErrNoProxiesProvided = errors.New("proxyflare: no proxies provided")
)

// RoundRobinProvider implements ProxyProvider with a round-robin strategy.
type RoundRobinProvider struct {
	proxies []*Proxy
	next    atomic.Uint32
}

// NewRoundRobinProvider creates a new RoundRobinProvider.
func NewRoundRobinProvider(proxies []*Proxy) *RoundRobinProvider {
	if len(proxies) == 0 {
		panic(ErrNoProxiesProvided)
	}
	return &RoundRobinProvider{
		proxies: append([]*Proxy(nil), proxies...),
	}
}

// Next returns the next available proxy from the pool or ErrNoAvailableProxies.
func (p *RoundRobinProvider) Next(now time.Time) (*Proxy, error) {
	if len(p.proxies) == 0 {
		return nil, ErrNoAvailableProxies
	}

	startPos := p.next.Add(1)

	// Try picking an available proxy via round-robin
	for i := range p.proxies {
		idx := int(startPos-1+uint32(i)) % len(p.proxies)
		if p.proxies[idx].IsAvailable(now) {
			return p.proxies[idx], nil
		}
	}

	return nil, ErrNoAvailableProxies
}
