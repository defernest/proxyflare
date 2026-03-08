package proxyflare_test

import (
	"context"
	"errors"
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

// TestTransport_RoundTrip_Success проверяет успешное выполнение запросов.
// Убеждается, что транспорт по стандарту Round-Robin чередует рабочие прокси,
// корректно обрабатывает недоступные узлы и перенаправляет X-Target-Url заголовок.
func TestTransport_RoundTrip_Success(t *testing.T) {
	var requestCount int32

	// Создаем тестовые HTTP-серверы, которые будут выступать в роли прокси.
	// Они проверяют наличие заголовка X-Target-Url и возвращают ответ.
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

	// Создаем транспорт с round-robin провайдером.
	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	require.NoError(t, err, "failed to create request")

	// P1 должен обслужить запрос, так как P2 недоступен
	resp1, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp1.Body.Close()

	body1, err := io.ReadAll(resp1.Body)
	require.NoError(t, err)
	require.Equal(t, "server1", string(body1))

	// Убеждаемся, что req не был мутирован
	require.Equal(t, "http://example.com/api", req.URL.String())
	require.Empty(t, req.Header.Get("X-Target-Url"))

	p2.SetAvailableAfter(0) // Теперь P2 доступен

	// Round-robin должен чередоваться
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
	require.True(t, receivedServers["server1"], "Expected server1 to be hit")
	// Должно быть 3 запроса, так как P2 был недоступен в первом запросе
	require.Equal(t, int32(3), atomic.LoadInt32(&requestCount))
}

// TestTransport_RoundTrip_NoProxiesAvailable проверяет поведение при недоступности прокси.
// Убеждается, что возвращается специфичная ошибка ErrNoAvailableProxies,
// если все прокси в пуле находятся во временном бане.
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

// TestNewTransport_EmptyOrNilProxies проверяет создание транспорта с пустым или nil пулом прокси.
// Убеждается, что конструктор NewRoundRobinProvider паникует при передаче nil или пустого списка прокси.
// Также проверяет, что транспорт корректно инициализируется и возвращает
// ErrNoAvailableProxies при попытке совершить RoundTrip с провайдером, который не имеет прокси.
func TestNewTransport_EmptyOrNilProxies(t *testing.T) {
	// Проверяем, что конструктор NewTransport паникует при передаче nil провайдера.
	require.PanicsWithValue(t, proxyflare.ErrProviderCannotBeNil, func() {
		proxyflare.NewTransport(nil, nil)
	})

	// Проверяем, что конструктор NewRoundRobinProvider паникует при передаче nil или пустого списка прокси.
	require.PanicsWithValue(t, proxyflare.ErrNoProxiesProvided, func() {
		proxyflare.NewRoundRobinProvider(nil)
	})
	require.PanicsWithValue(t, proxyflare.ErrNoProxiesProvided, func() {
		proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{})
	})
	// Проверяем, что транспорт корректно инициализируется и возвращает
	// ErrNoAvailableProxies при попытке совершить RoundTrip с провайдером, который не имеет прокси.
	tr := proxyflare.NewTransport(&proxyflare.RoundRobinProvider{}, nil)
	require.NotNil(t, tr.Base(), "expected base round-tripper")
	require.NotNil(t, tr.Provider(), "expected provider")

	req, err := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	require.NoError(t, err)
	_, err = tr.RoundTrip(req)
	require.ErrorIs(t, err, proxyflare.ErrNoAvailableProxies)
}

// TestTransport_RoundTrip_NilHeader проверяет защиту от nil-карт в заголовках.
// Убеждается, что если вызывающий код передал запрос с `req.Header = nil`,
// транспорт корректно создаст мапу перед добавлением X-Target-Url и не вызовет панику.
func TestTransport_RoundTrip_NilHeader(t *testing.T) {
	// Создаем тестовый HTTP-сервер, который будет выступать в роли прокси.
	// Он проверяет наличие заголовка X-Target-Url и возвращает ответ.
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

	// Устанавливаем nil в качестве заголовка.
	req.Header = nil

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestProxy_BanFor проверяет механизм локальной блокировки отдельного прокси.
// Убеждается, что вызов BanFor делает прокси недоступным (IsAvailable = false)
// на указанное время, а затем оно снова восстанавливается.
func TestProxy_BanFor(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1")
	p := proxyflare.NewProxy(u)

	// Проверяем, что прокси доступен по умолчанию.
	assert.True(t, p.IsAvailable(time.Now()))

	// Блокируем прокси на 1 минуту.
	p.BanFor(time.Minute)

	// Проверяем, что прокси недоступен.
	assert.False(t, p.IsAvailable(time.Now()))

	// Проверяем, что прокси доступен через 61 секунду.
	assert.True(t, p.IsAvailable(time.Now().Add(61*time.Second)))
}

// TestStatusCodeChecker проверяет работу хелпера StatusCodeChecker.
// Убеждается, что в нем корректно фильтруются HTTP-статусы и
// он безопасно обрабатывает `nil` ответы от клиента.
func TestStatusCodeChecker(t *testing.T) {
	checker := proxyflare.StatusCodeChecker(http.StatusTooManyRequests, http.StatusServiceUnavailable)

	assert.True(t, checker(nil, &http.Response{StatusCode: http.StatusTooManyRequests}, nil))
	assert.True(t, checker(nil, &http.Response{StatusCode: http.StatusServiceUnavailable}, nil))
	assert.False(t, checker(nil, &http.Response{StatusCode: http.StatusOK}, nil))
	assert.False(t, checker(nil, nil, nil), "Should return false for nil response")
}

// TestTransport_AutoBan проверяет корректность фильтрации AutoBan правил.
// Убеждается, что при получении статуса 429 от целевого сервера,
// сработает правило авто-бана и заблокирует конкретный прокси на 1 минуту.
func TestTransport_AutoBan(t *testing.T) {
	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	p1 := proxyflare.NewProxy(u1)

	proxies := []*proxyflare.Proxy{p1}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

	// Since it returned 429, the proxy should be banned for 1 minute
	assert.False(t, p1.IsAvailable(time.Now()), "Proxy should be banned")
}

// TestTransport_Retry проверяет логику встроенных ретраев на лету.
// Если первый прокси получил 429 и ушел в бан, транспорт должен прозрачно для клиента
// повторить запрос на следующем прокси в пуле и вернуть уже успешный результат.
func TestTransport_Retry(t *testing.T) {
	var requestCount int32

	// Создаем тестовые HTTP-серверы, которые будут выступать в роли прокси.
	// Первый сервер будет возвращать 429, второй - 200 (с телом "server2-ok").
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

	// Создаем транспорт с round-robin провайдером, авто-баном и ретраями.
	proxies := []*proxyflare.Proxy{p1, p2}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(2)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)

	// Выполняем запрос.
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Проверяем, что тело ответа корректное и статус 200 пришел от второго прокси.
	body, _ := io.ReadAll(resp.Body)
	require.Equal(t, "server2-ok", string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Проверяем, что было сделано ровно 2 запроса (первый - неудачный, второй - успешный).
	assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount), "It should make exactly two requests")
	// Проверяем, что первый прокси забанен, а второй доступен.
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
	assert.True(t, p2.IsAvailable(time.Now()), "Server 2 proxy should still be available")
}

// TestTransport_Retry_WithBody проверяет работу ретраев для запросов с телом (POST/PUT).
// Убеждается, что функция GetBody используется корректно и позволяет отправить
// идентичное тело запроса на оба прокси при повторной попытке.
func TestTransport_Retry_WithBody(t *testing.T) {
	var requestCount int32
	bodyContent := "my-test-body"

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		// Проверяем, что тело запроса соответствует отправленному.
		assert.Equal(t, bodyContent, string(body), "Body on first attempt should match")

		// Возвращаем 429, чтобы запустить ретрай.
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		// Проверяем, что тело запроса соответствует отправленному (должно быть идентично).
		assert.Equal(t, bodyContent, string(body), "Body on second attempt should also match")

		// Возвращаем 200, чтобы завершить запрос.
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
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(2)

	// stdlib `http.NewRequest` автоматически заполняет GetBody, если передан `strings.Reader`.
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", strings.NewReader(bodyContent))

	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// Проверяем, что тело ответа корректное и статус 200 пришел от второго прокси.
	require.Equal(t, "server2-ok", string(body))
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Проверяем, что было сделано ровно 2 запроса (первый - неудачный, второй - успешный).
	assert.Equal(t, int32(2), atomic.LoadInt32(&requestCount), "It should make exactly two requests")
	// Проверяем, что первый прокси забанен, а второй доступен.
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
	assert.True(t, p2.IsAvailable(time.Now()), "Server 2 proxy should still be available")
}

// TestTransport_Retry_NoProxiesAvailable проверяет логику ретраев при исчерпании пула.
// Убеждается, что если кончились доступные прокси для повторной попытки,
// транспорт не зависнет, а вернёт последнюю зафиксированную ошибку (например, статус 429).
func TestTransport_Retry_NoProxiesAvailable(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	p1 := proxyflare.NewProxy(u1)

	// Создаем транспорт с одним прокси и авто-баном, но с maxRetries=2.
	proxies := []*proxyflare.Proxy{p1}
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider(proxies), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(2)

	req, _ := http.NewRequest(http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Ответ от первого неудачного попытка (429): возвращается последняя зафиксированная ошибка.
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	// Проверяем, что был сделан ровно один запрос.
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
	// Проверяем, что единственнй прокси забанен.
	assert.False(t, p1.IsAvailable(time.Now()), "Server 1 proxy should be banned")
}

// TestTransport_Retry_NilGetBody проверяет отказоустойчивость при невозможности воссоздать тело.
// Убеждается, что ретрай прерывается, и возвращается оригинальный ответ от первого прокси,
// если у реквеста нет функции GetBody (тело невозможно прочитать дважды).
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
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(2)

	// Создадим запрос с телом, но без GetBody
	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", io.NopCloser(strings.NewReader("dummy")))
	req.GetBody = nil // Уберем GetBody

	resp, err := tr.RoundTrip(req)

	// Должен прервать ретрай и вернуть ответ от первого попытка (429) немедленно
	// вместо возвращения ошибки о GetBody, как это реализовано в логике.
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount), "It should only make 1 request because GetBody was nil")
}

// TestTransport_Retry_GetBodyError проверяет отработку ошибки при генерации тела запроса.
// Убеждается, что если функция GetBody вернула ошибку при попытке повторить запрос,
// цикл прервется и метод мягко вернет последний ответ (с первого прокси, например статус 429),
// а не панику или ошибку самого реквеста.
func TestTransport_Retry_GetBodyError(t *testing.T) {
	var requestCount int32

	proxyServer1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer proxyServer1.Close()

	u1, _ := url.Parse(proxyServer1.URL)
	p1 := proxyflare.NewProxy(u1)

	proxyServer2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer proxyServer2.Close()
	u2, _ := url.Parse(proxyServer2.URL)
	p2 := proxyflare.NewProxy(u2)

	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{p1, p2}), nil).
		WithAutoBan(proxyflare.StatusCodeChecker(http.StatusTooManyRequests), time.Minute).
		WithRetry(2)

	req, _ := http.NewRequest(http.MethodPost, "http://example.com/api", io.NopCloser(strings.NewReader("dummy")))

	// Подменяем GetBody кастомной функцией, которая симулирует ошибку чтения (например, файла).
	req.GetBody = func() (io.ReadCloser, error) {
		return nil, errors.New("simulated GetBody error")
	}

	resp, err := tr.RoundTrip(req)

	// Ожидаем, что транспорт перехватил ошибку GetBody на второй попытке
	// и изящно вернул нам ответ от первой неудачной попытки (429 Too Many Requests).
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	// Должна была состояться ровно 1 реальная HTTP-попытка (на вторую не хватило тела)
	assert.Equal(t, int32(1), atomic.LoadInt32(&requestCount))
}

// TestTransport_RoundTrip_ContextCancelledBeforeAttempt проверяет отмену запроса еще до 1 попытки.
// Убеждается, что если контекст был отменен еще до начала RoundTrip (на `attempt == 0`),
// транспорт корректно прервет работу и вернет ошибку контекста, не переходя к реальным запросам.
func TestTransport_RoundTrip_ContextCancelledBeforeAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Отменяем контекст до начала работы

	p := proxyflare.NewProxy(&url.URL{Scheme: "http", Host: "127.0.0.1"})
	tr := proxyflare.NewTransport(proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{p}), nil)

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/api", nil)
	resp, err := tr.RoundTrip(req)

	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, resp)
}

// cancelProvider оборачивает ProxyProvider для отмены контекста на втором вызове Next.
type cancelProvider struct {
	proxyflare.ProxyProvider

	cancel context.CancelFunc
	calls  int
}

func (cp *cancelProvider) Next(now time.Time) (*proxyflare.Proxy, error) {
	cp.calls++
	if cp.calls == 2 {
		cp.cancel() // Отменяем контекст прямо перед второй попыткой (retry)
	}
	return cp.ProxyProvider.Next(now)
}

// TestTransport_Retry_ContextCancelled проверяет поддержку отмены по контексту.
// Убеждается, что если Context отменяется прямо во время ретраев,
// транспорт моментально прерывает попытки и возвращает `context.Canceled`.
func TestTransport_Retry_ContextCancelled(t *testing.T) {
	var requestCount int32

	ctx, cancel := context.WithCancel(context.Background())

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

	proxies := []*proxyflare.Proxy{proxyflare.NewProxy(u1), proxyflare.NewProxy(u2)}
	baseProvider := proxyflare.NewRoundRobinProvider(proxies)

	// Создаем обертку над ProxyProvider, которая отменяет контекст ровно перед
	// подготовкой ко второй попытке, чтобы гарантированно покрыть логику reqErr
	cp := &cancelProvider{ProxyProvider: baseProvider, cancel: cancel}

	tr := proxyflare.NewTransport(cp, nil).
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

// TestTransport_AutoBan_MultipleRules_MaxDurationWins проверяет работу с пересекающимися правилами.
// Убеждается, что если сработало более одного правила бана для ответа,
// прокси будет заблокирован на максимальное время (maxDuration) из всех сработавших.
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

	// Должен быть забанен на 5 минут, а не на 1 минуту
	assert.False(t, p.IsAvailable(time.Now()))
	assert.False(t, p.IsAvailable(time.Now().Add(2*time.Minute)), "Should still be banned after 2 minutes")
	assert.True(t, p.IsAvailable(time.Now().Add(6*time.Minute)), "Should be available after 6 minutes")
}

// TestTransport_AutoBan_PanicsOnZeroDuration проверяет строгую валидацию конфигурации.
// Убеждается, что метод `WithAutoBan` падает с паникой, если передать пустое правило
// или отрицательную/нулевую длительность для блокировки (duration <= 0).
func TestTransport_AutoBan_PanicsOnZeroDuration(t *testing.T) {
	provider := proxyflare.NewRoundRobinProvider([]*proxyflare.Proxy{proxyflare.NewProxy(&url.URL{})})
	tr := proxyflare.NewTransport(provider, nil)

	checker := proxyflare.StatusCodeChecker(http.StatusTooManyRequests)

	assert.PanicsWithValue(t, proxyflare.ErrDurationCannotBeZero, func() {
		tr.WithAutoBan(checker, 0)
	})

	assert.PanicsWithValue(t, proxyflare.ErrDurationCannotBeZero, func() {
		tr.WithAutoBan(checker, -1*time.Minute)
	})

	assert.PanicsWithValue(t, proxyflare.ErrCheckerCannotBeNil, func() {
		tr.WithAutoBan(nil, time.Minute)
	})
}
