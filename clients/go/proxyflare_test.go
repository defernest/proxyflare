package proxyflare_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	proxyflare "github.com/defernest/proxyflare/clients/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransport_RoundTrip_Success(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		targetURL := r.Header.Get("X-Target-Url")
		assert.Equal(t, "http://example.com/api", targetURL, "Target URL header must match")

		_, err := io.WriteString(w, "server1")
		assert.NoError(t, err)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		targetURL := r.Header.Get("X-Target-Url")
		assert.Equal(t, "http://example.com/api", targetURL, "Target URL header must match")

		_, err := io.WriteString(w, "server2")
		assert.NoError(t, err)
	}))
	defer proxyServer2.Close()

	u1, err := url.Parse(proxyServer1.URL)
	require.NoError(t, err)
	u2, err := url.Parse(proxyServer2.URL)
	require.NoError(t, err)

	p1 := proxyflare.NewProxy(u1)
	p2 := proxyflare.NewProxy(u2)
	p2.SetAvailableAfter(time.Now().Unix() + 100) // initially unavailable

	// Transport configuration
	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	require.NoError(t, err, "failed to create request")

	// P1 should serve the request since P2 is unavailable
	resp1, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp1.Body.Close()

	body1, err := io.ReadAll(resp1.Body)
	require.NoError(t, err)
	require.Equal(t, "server1", string(body1))

	// Ensure req was not mutated
	require.Equal(t, "http://example.com/api", req.URL.String())
	require.Empty(t, req.Header.Get("X-Target-Url"))

	p2.SetAvailableAfter(0) // Now P2 is available

	// Round-robin should alternate
	resp2, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp2.Body.Close()
	body2, err := io.ReadAll(resp2.Body)
	require.NoError(t, err)

	resp3, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp3.Body.Close()
	body3, err := io.ReadAll(resp3.Body)
	require.NoError(t, err)

	receivedServers := map[string]bool{
		string(body1): true,
		string(body2): true,
		string(body3): true,
	}
	require.True(t, receivedServers["server1"], "Expected server1 to be hit")
	require.True(t, receivedServers["server2"], "Expected server2 to be hit")
	require.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

func TestTransport_RoundTrip_NoProxiesAvailable(t *testing.T) {
	p := proxyflare.NewProxy(&url.URL{Scheme: "http", Host: "127.0.0.1"})
	p.SetAvailableAfter(time.Now().Unix() + 100)

	proxies := []*proxyflare.Proxy{p}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	require.NoError(t, err)

	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, proxyflare.ErrNoAvailableProxies)
}

func TestNewTransport_EmptyProxies(t *testing.T) {
	provider := proxyflare.NewRoundRobinProvider(nil)
	tr := proxyflare.NewTransport(provider, nil)
	require.NotNil(t, tr.Base(), "expected base round-tripper")
	require.NotNil(t, tr.Provider(), "expected provider")

	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, proxyflare.ErrNoAvailableProxies)
}

func TestTransport_RoundTrip_NilHeader(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "http://example.com/test", r.Header.Get("X-Target-Url"))
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer.Close()

	u, err := url.Parse(proxyServer.URL)
	require.NoError(t, err)

	p := proxyflare.NewProxy(u)
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{p}), nil)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/test", nil)
	require.NoError(t, err)

	// Explicitly set Header to nil
	req.Header = nil

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestProxy_BanFor(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1")
	p := proxyflare.NewProxy(u)

	// Initially available
	assert.True(t, p.IsAvailable(time.Now()))

	// Ban for 1 minute
	p.BanFor(time.Minute)

	// Should be unavailable now
	assert.False(t, p.IsAvailable(time.Now()))

	// Should be available after 61 seconds
	assert.True(t, p.IsAvailable(time.Now().Add(61*time.Second)))
}

func TestTransport_AutoBan(t *testing.T) {
	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	p1 := proxyflare.NewProxy(u1)

	proxies := []*proxyflare.Proxy{p1}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(func(_ *http.Request, resp *http.Response, _ error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}, time.Minute)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

	// Since it returned 429, the proxy should be banned for 1 minute
	assert.False(t, p1.IsAvailable(time.Now()), "Proxy should be banned")
}

func TestTransport_Retry(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "server2-ok")
	}))
	defer proxyServer2.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	u2, _ := url.Parse(proxyServer2.URL)
	p1 := proxyflare.NewProxy(u1)
	p2 := proxyflare.NewProxy(u2)

	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(func(_ *http.Request, resp *http.Response, _ error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}, time.Minute).
		WithRetry(2)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "server2-ok", string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount), "It should make exactly two requests")
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
	assert.True(t, p2.IsAvailable(time.Now()), "Server 2 proxy should still be available")
}

func TestTransport_Retry_WithBody(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "my-test-body", string(body), "Body on first attempt should match")

		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.Equal(t, "my-test-body", string(body), "Body on second attempt should also match")

		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "server2-ok")
	}))
	defer proxyServer2.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	u2, _ := url.Parse(proxyServer2.URL)
	p1 := proxyflare.NewProxy(u1)
	p2 := proxyflare.NewProxy(u2)

	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(func(_ *http.Request, resp *http.Response, _ error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}, time.Minute).
		WithRetry(2)

	// stdlib `http.NewRequest` automatically populates GetBody if a `strings.Reader` is provided.
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader("my-test-body"))

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "server2-ok", string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount), "It should make exactly two requests")
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
	assert.True(t, p2.IsAvailable(time.Now()), "Server 2 proxy should still be available")
}

func TestTransport_Retry_NoProxiesAvailable(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	p1 := proxyflare.NewProxy(u1)

	// Since maxRetries=1, it will try to get a new proxy but the provider will error
	// because p1 and only p1 is in the pool and it will get banned.
	proxies := []*proxyflare.Proxy{p1}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(func(_ *http.Request, resp *http.Response, _ error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}, time.Minute).
		WithRetry(2)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)

	// In the new implementation pt.provider.Next(...) returns ErrNoAvailableProxies,
	// and because attempt > 0, RoundTrip returns `lastResp, lastErr`.
	require.NoError(t, err)
	defer resp.Body.Close()

	// It should return the response from the first failed attempt (429)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
}

func TestTransport_Retry_NilGetBody(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer2.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	u2, _ := url.Parse(proxyServer2.URL)
	p1 := proxyflare.NewProxy(u1)
	p2 := proxyflare.NewProxy(u2)

	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(func(_ *http.Request, resp *http.Response, _ error) bool {
			return resp != nil && resp.StatusCode == http.StatusTooManyRequests
		}, time.Minute).
		WithRetry(2)

	// Create a dummy body io.ReadCloser WITHOUT GetBody
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", io.NopCloser(strings.NewReader("dummy")))
	req.GetBody = nil // Explicitly remove GetBody just in case

	resp, err := tr.RoundTrip(req)

	// It should prevent retry and return the response from the 1st attempt (429) immediately
	// rather than returning an error about GetBody, per the implementation logic.
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount), "It should only make 1 request because GetBody was nil")
}

func TestTransport_Retry_ContextCancelled(t *testing.T) {
	var requestCount int32

	ctx, cancel := context.WithCancel(context.Background())

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		cancel() // Cancel context after first request is received
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer2.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	u2, _ := url.Parse(proxyServer2.URL)

	proxies := []*proxyflare.Proxy{proxyflare.NewProxy(u1), proxyflare.NewProxy(u2)}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(3)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/api", nil)

	resp, err := tr.RoundTrip(req)

	require.ErrorIs(t, err, context.Canceled)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount),
		"Should only make 1 request before context cancellation aborts retries")
}

func TestTransport_AutoBan_MultipleRules_MaxDurationWins(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer.Close()

	u, _ := url.Parse(proxyServer.URL)
	p := proxyflare.NewProxy(u)

	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{p}), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), 5*time.Minute)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Should be banned for 5 minutes (the longer duration), not 1 minute
	assert.False(t, p.IsAvailable(time.Now()))
	assert.False(t, p.IsAvailable(time.Now().Add(2*time.Minute)), "Should still be banned after 2 minutes")
	assert.True(t, p.IsAvailable(time.Now().Add(6*time.Minute)), "Should be available after 6 minutes")
}

func TestStatusCodeChecker(t *testing.T) {
	checker := proxyflare.StatusCodeChecker(http.StatusTooManyRequests, http.StatusServiceUnavailable)

	assert.True(t, checker(nil, &http.Response{StatusCode: http.StatusTooManyRequests}, nil))
	assert.True(t, checker(nil, &http.Response{StatusCode: http.StatusServiceUnavailable}, nil))
	assert.False(t, checker(nil, &http.Response{StatusCode: http.StatusOK}, nil))
	assert.False(t, checker(nil, nil, nil), "Should return false for nil response")
}
