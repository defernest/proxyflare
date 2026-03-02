package proxyflare_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
