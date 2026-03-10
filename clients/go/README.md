# proxyflare/go

Go client library for [Proxyflare](https://github.com/defernest/proxyflare) — transparent HTTP proxying through Cloudflare Workers with round-robin rotation, auto-ban, and retry support.

## Installation

```bash
go get github.com/defernest/proxyflare/clients/go
```

## Quick Start

```go
package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	proxyflare "github.com/defernest/proxyflare/clients/go"
)

func main() {
	u1, _ := url.Parse("https://worker-1.your-subdomain.workers.dev")
	u2, _ := url.Parse("https://worker-2.your-subdomain.workers.dev")

	provider, err := proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{
		proxyflare.NewProxy(u1),
		proxyflare.NewProxy(u2),
	})
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{
		Transport: proxyflare.NewTransport(provider, nil).
			WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), 5*time.Minute).
			WithRetry(3),
	}

	resp, err := client.Get("https://api.ipify.org?format=json")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	fmt.Println("Status:", resp.StatusCode)
}
```

## Features

- **Round-Robin Rotation** — requests are distributed evenly across the proxy pool
- **Auto-Ban** — automatically bans proxies returning specific HTTP status codes (e.g., 429)
- **Retry** — transparent retry on the next proxy when a ban is triggered
- **Dynamic Proxy Pool** — update proxies at runtime via `SetProxies()` without recreating the client
- **Lock-Free Concurrency** — `atomic.Pointer` based thread safety with zero allocations on the read path

## API Reference

### Transport

`Transport` implements `http.RoundTripper`. It rewrites `req.URL` to the selected proxy worker and passes the original target URL in the `X-Target-Url` header.

```go
// Create transport with a custom base RoundTripper (nil = http.DefaultTransport)
tr := proxyflare.NewTransport(provider, nil)

// Add auto-ban: ban proxy for 5 min on 429 responses
tr.WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), 5*time.Minute)

// Enable retries (3 total attempts)
tr.WithRetry(3)
```

### RoundRobinProvider

Thread-safe round-robin proxy provider with dynamic pool updates.

```go
// Create provider
provider, err := proxyflare.NewRoundRobinProvider(proxies)

// Get next available proxy
proxy, err := provider.Next(time.Now())

// Update proxy pool at runtime (thread-safe)
err = provider.SetProxies(newProxies)

// Get a copy of current proxies
current := provider.GetProxies()
```

### Proxy

Represents a single proxy worker with atomic availability tracking.

```go
proxy := proxyflare.NewProxy(parsedURL)

proxy.IsAvailable(time.Now())   // Check availability
proxy.BanFor(5 * time.Minute)   // Temporarily ban
proxy.Address()                 // Get worker URL
```

## Custom ProxyProvider

Implement the `ProxyProvider` interface for custom selection strategies:

```go
type ProxyProvider interface {
    Next(now time.Time) (*Proxy, error)
}
```
