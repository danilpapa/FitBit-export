package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fitbit-export/internal/config"
)

func newTestClient() *Client {
	client := New(testConfig())
	client.accessToken = "token"
	client.baseDelay = time.Millisecond
	return client
}

func post(t *testing.T, ctx context.Context, url string, body string) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func TestDoRetriesTransientStatuses(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				if string(body) != "payload" {
					t.Errorf("attempt %d body = %q, want it resent", calls.Load()+1, body)
				}
				if calls.Add(1) < 3 {
					w.WriteHeader(status)
				}
			}))
			defer server.Close()

			response, err := newTestClient().Do(post(t, context.Background(), server.URL, "payload"))
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK || calls.Load() != 3 {
				t.Fatalf("status = %d after %d calls, want 200 after 3", response.StatusCode, calls.Load())
			}
		})
	}
}

func TestDoRetriesNetworkErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
		}
	}))
	defer server.Close()

	response, err := newTestClient().Do(post(t, context.Background(), server.URL, "payload"))
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("status = %d after %d calls, want 200 after 2", response.StatusCode, calls.Load())
	}
}

func TestDoStopsAfterMaxAttempts(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	response, err := newTestClient().Do(post(t, context.Background(), server.URL, "payload"))
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusServiceUnavailable || calls.Load() != maxAttempts {
		t.Fatalf("status = %d after %d calls, want 503 after %d", response.StatusCode, calls.Load(), maxAttempts)
	}
}

func TestDoDoesNotRetryPermanentErrors(t *testing.T) {
	for _, status := range []int{400, 403, 404} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()

			response, err := newTestClient().Do(post(t, context.Background(), server.URL, "payload"))
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != status || calls.Load() != 1 {
				t.Fatalf("status = %d after %d calls, want %d after 1", response.StatusCode, calls.Load(), status)
			}
		})
	}
}

func TestDoStopsWaitingWhenCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := newTestClient()
	client.baseDelay = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.Do(post(t, ctx, server.URL, "payload"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Do() error = %v, want context deadline", err)
	}
}

func TestRefreshRetriesTokenRequest(t *testing.T) {
	var calls atomic.Int32
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "grant_type=refresh_token") {
			t.Errorf("attempt %d body = %q, want it resent", calls.Load()+1, body)
		}
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"access_token":"fresh"}`))
	}))
	defer tokens.Close()

	client := New(testConfig())
	client.tokenURL = tokens.URL
	client.baseDelay = time.Millisecond

	token, err := client.refreshAccessToken(context.Background())
	if err != nil {
		t.Fatalf("refreshAccessToken() error = %v", err)
	}
	if token != "fresh" || calls.Load() != 2 {
		t.Fatalf("token = %q after %d calls", token, calls.Load())
	}
}

func TestRefreshDoesNotRetryRevokedToken(t *testing.T) {
	var calls atomic.Int32
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer tokens.Close()

	client := New(testConfig())
	client.tokenURL = tokens.URL

	if _, err := client.refreshAccessToken(context.Background()); err == nil {
		t.Fatal("refreshAccessToken() error = nil, want HTTP 400")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter string
		want       time.Duration
	}{
		{name: "no header", want: time.Second},
		{name: "seconds", retryAfter: "5", want: 5 * time.Second},
		{name: "capped", retryAfter: "3600", want: maxDelay},
		{name: "http date falls back", retryAfter: "Sat, 31 Oct 2026 12:00:00 GMT", want: time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := &http.Response{Header: http.Header{}}
			if test.retryAfter != "" {
				response.Header.Set("Retry-After", test.retryAfter)
			}
			if got := retryDelay(response, time.Second); got != test.want {
				t.Fatalf("retryDelay() = %s, want %s", got, test.want)
			}
		})
	}
}

func testConfig() config.Config {
	return config.Config{ClientID: "id", ClientSecret: "secret", RefreshToken: "refresh"}
}
