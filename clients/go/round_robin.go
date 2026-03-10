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
	proxies atomic.Pointer[[]*Proxy]
	next    atomic.Uint32
}

// NewRoundRobinProvider creates a new RoundRobinProvider.
func NewRoundRobinProvider(proxies []*Proxy) (*RoundRobinProvider, error) {
	if len(proxies) == 0 {
		return nil, ErrNoProxiesProvided
	}
	p := &RoundRobinProvider{}
	copied := append([]*Proxy(nil), proxies...)
	p.proxies.Store(&copied)
	return p, nil
}

// Next returns the next available proxy from the pool or ErrNoAvailableProxies.
func (p *RoundRobinProvider) Next(now time.Time) (*Proxy, error) {
	current := p.proxies.Load()
	if current == nil || len(*current) == 0 {
		return nil, ErrNoAvailableProxies
	}

	list := *current
	startPos := p.next.Add(1)

	// Try picking an available proxy via round-robin
	for i := range list {
		idx := int(startPos-1+uint32(i)) % len(list)
		if list[idx].IsAvailable(now) {
			return list[idx], nil
		}
	}

	return nil, ErrNoAvailableProxies
}

// SetProxies atomically updates the list of proxies in the provider.
// Returns ErrNoProxiesProvided if the provided list is empty.
func (p *RoundRobinProvider) SetProxies(proxies []*Proxy) error {
	if len(proxies) == 0 {
		return ErrNoProxiesProvided
	}
	copied := append([]*Proxy(nil), proxies...)
	p.proxies.Store(&copied)
	return nil
}

// GetProxies returns a copy of the current list of proxies in the provider.
func (p *RoundRobinProvider) GetProxies() []*Proxy {
	current := p.proxies.Load()
	if current == nil {
		return nil
	}
	return append([]*Proxy(nil), (*current)...)
}
