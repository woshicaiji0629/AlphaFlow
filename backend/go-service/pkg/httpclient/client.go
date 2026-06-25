package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	client     *http.Client
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func New() *Client {
	return &Client{
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: defaultTransport(),
		},
		maxRetries: 3,
		baseDelay:  300 * time.Millisecond,
		maxDelay:   2 * time.Second,
	}
}

type HTTPError struct {
	StatusCode int
	Status     string
	Body       []byte
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http status %s", e.Status)
}

func (c *Client) Get(
	ctx context.Context,
	rawURL string,
	query map[string]string,
) ([]byte, error) {
	endpoint, err := withQuery(rawURL, query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create get request: %w", err)
	}
	return c.DoBytes(req)
}

func (c *Client) Post(
	ctx context.Context,
	rawURL string,
	query map[string]string,
	body []byte,
	headers map[string]string,
) ([]byte, error) {
	endpoint, err := withQuery(rawURL, query)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create post request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return c.DoBytes(req)
}

func (c *Client) DoBytes(req *http.Request) ([]byte, error) {
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       body,
		}
	}
	return body, nil
}

func (c *Client) Do(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			if err := sleep(req.Context(), c.delay(attempt)); err != nil {
				return nil, err
			}
		}

		resp, err := c.client.Do(req)
		if err != nil {
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			lastErr = err
			continue
		}

		if !retryableStatus(resp.StatusCode) {
			return resp, nil
		}

		lastErr = fmt.Errorf("retryable http status %s", resp.Status)
		resp.Body.Close()
	}

	return nil, lastErr
}

func defaultTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func withQuery(rawURL string, query map[string]string) (string, error) {
	endpoint, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parse url: %w", err)
	}
	values := endpoint.Query()
	for key, value := range query {
		values.Set(key, value)
	}
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), nil
}

func (c *Client) delay(attempt int) time.Duration {
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(c.baseDelay) * multiplier)
	if delay > c.maxDelay {
		return c.maxDelay
	}
	return delay
}

func retryableStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
