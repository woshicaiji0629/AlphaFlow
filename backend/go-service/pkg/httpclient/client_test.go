package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryableStatus(t *testing.T) {
	tests := map[int]bool{
		http.StatusOK:                  false,
		http.StatusBadRequest:          false,
		http.StatusTooManyRequests:     true,
		http.StatusInternalServerError: true,
		http.StatusBadGateway:          true,
	}

	for status, want := range tests {
		if got := retryableStatus(status); got != want {
			t.Fatalf("retryableStatus(%d) = %v, want %v", status, got, want)
		}
	}
}

func TestSleepReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := sleep(ctx, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep error = %v, want context canceled", err)
	}
}

func TestGetReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("symbol"); got != "ETHUSDT" {
			t.Fatalf("symbol query = %q, want ETHUSDT", got)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	body, err := New().Get(context.Background(), server.URL, map[string]string{
		"symbol": "ETHUSDT",
	})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %s", body)
	}
}

func TestPostReturnsBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Test"); got != "1" {
			t.Fatalf("X-Test header = %q, want 1", got)
		}
		_, _ = w.Write([]byte(`posted`))
	}))
	defer server.Close()

	body, err := New().Post(
		context.Background(),
		server.URL,
		nil,
		[]byte(`payload`),
		map[string]string{"X-Test": "1"},
	)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if string(body) != "posted" {
		t.Fatalf("body = %s", body)
	}
}

func TestGetReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer server.Close()

	_, err := New().Get(context.Background(), server.URL, nil)
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %v, want HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("status code = %d, want 400", httpErr.StatusCode)
	}
}
