// Package proxyflare implements a client for ProxyFlare (Cloudflare Workers based) proxy distribution.
// Also implements a round-robin proxy provider, for proxy rotation.
// See https://github.com/defernest/proxyflare for more information.
package proxyflare

import (
	"errors"
	"net/http"
	"time"
)

// ProxyProvider defines an interface for providing proxies.
type ProxyProvider interface {
	// Next returns the next available proxy from the pool or ErrNoAvailableProxies.
	Next(now time.Time) (*Proxy, error)
}

// Checker is a function that checks if a proxy should be banned based on the response or error.
type Checker func(req *http.Request, resp *http.Response, err error) bool

// StatusCodeChecker returns a [Checker] that triggers a ban on specific HTTP status codes.
func StatusCodeChecker(codes ...int) Checker {
	set := make(map[int]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return func(_ *http.Request, resp *http.Response, _ error) bool {
		if resp == nil {
			return false
		}
		_, ok := set[resp.StatusCode]
		return ok
	}
}

type autoBanRule struct {
	checker  Checker
	duration time.Duration
}

// Transport implements the [http.RoundTripper] interface.
// It uses round-robin to distribute requests across available proxy workers.
// Target URL from `req.URL` is sent to proxy-workers by request header `X-Target-Url`.
type Transport struct {
	base     http.RoundTripper
	provider ProxyProvider

	autoBanRules []autoBanRule
	maxAttempts  int
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
		provider:    provider,
		base:        base,
		maxAttempts: 1, // Default to 1 attempt (no retries)
	}
	return t
}

// WithAutoBan adds an auto-ban rule. If the checker returns true for a response or error,
// the currently selected proxy will be banned for the specified duration.
func (pt *Transport) WithAutoBan(checker Checker, duration time.Duration) *Transport {
	if checker == nil {
		panic("proxyflare: checker cannot be nil")
	}
	pt.autoBanRules = append(pt.autoBanRules, autoBanRule{
		checker:  checker,
		duration: duration,
	})
	return pt
}

// WithRetry configures the transport to automatically retry the request on a new proxy
// if an auto-ban rule is triggered. maxAttempts specifies the total number of attempts
// (e.g., maxAttempts=3 means the initial attempt plus 2 retries).
// If not called, the transport defaults to a single attempt (no retries).
//
// Note on Request Body:
// If the original request has a non-nil Body, a retry can only be performed if req.GetBody is not nil.
// This is because the body is consumed during the first attempt. If req.GetBody is nil, the transport
// cannot recreate the body and will return the error/response immediately without retrying.
// As defined by stdlib [http.NewRequestWithContext], requests with strings, bytes, and [strings.Reader] bodies
// will have GetBody automatically populated.
func (pt *Transport) WithRetry(maxAttempts int) *Transport {
	if maxAttempts > 1 {
		pt.maxAttempts = maxAttempts
	}
	return pt
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
	var lastResp *http.Response
	var lastErr error

	for attempt := range pt.maxAttempts {
		selectedProxy, err := pt.provider.Next(time.Now())
		if err != nil {
			// If we can't get a proxy, we must return the last error/response if we had one,
			// or the error from the provider.
			if attempt > 0 {
				return lastResp, lastErr
			}
			return nil, err
		}

		clonedReq, reqErr := pt.prepareRequest(req, attempt, selectedProxy)
		if reqErr != nil {
			// If we fail to prepare the request (e.g., GetBody failed), return the previous result
			if attempt > 0 {
				return lastResp, lastErr
			}
			return nil, reqErr
		}

		// Close the previous response body before making a new request
		if lastResp != nil && lastResp.Body != nil {
			_ = lastResp.Body.Close()
		}

		// Execute the request
		resp, roundTripErr := pt.base.RoundTrip(clonedReq)
		lastResp = resp
		lastErr = roundTripErr

		// Check auto-ban rules
		shouldBan, banDuration := pt.evaluateAutoBanRules(req, resp, roundTripErr)

		if !shouldBan {
			// If we didn't ban the proxy, the request is considered as processed (success or normal error)
			return resp, roundTripErr
		}

		selectedProxy.BanFor(banDuration)

		// If we need to retry but cannot recreate the request body, abort retries
		// and return the current response/error to the caller so they get the actual API error (e.g. 429).
		if req.Body != nil && req.GetBody == nil {
			break
		}
	}

	// If we exhausted all attempts, return the last result
	return lastResp, lastErr
}

func (pt *Transport) prepareRequest(req *http.Request, attempt int, selectedProxy *Proxy) (*http.Request, error) {
	if attempt > 0 {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
	}

	// Clone the request to avoid modifying the original one per http.RoundTripper contract
	clonedReq := req.Clone(req.Context())

	// If this is a retry and we have a body, we need to rebuild it
	if attempt > 0 && req.Body != nil {
		if req.GetBody == nil {
			// We can't retry because we can't recreate the request body.
			return nil, errors.New("cannot retry request with nil GetBody")
		}
		newBody, getBodyErr := req.GetBody()
		if getBodyErr != nil {
			// Failed to GetBody, can't proceed with retry
			return nil, getBodyErr
		}
		clonedReq.Body = newBody
	}

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

	return clonedReq, nil
}

func (pt *Transport) evaluateAutoBanRules(req *http.Request, resp *http.Response, err error) (bool, time.Duration) {
	var maxDuration time.Duration
	for _, rule := range pt.autoBanRules {
		if rule.checker(req, resp, err) {
			if rule.duration > maxDuration {
				maxDuration = rule.duration
			}
		}
	}
	return maxDuration > 0, maxDuration
}
