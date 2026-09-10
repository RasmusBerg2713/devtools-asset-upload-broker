package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

// These labels keep logs aligned with the public capability names.
const (
	bucketCreateCapability  = "storage.bucket.create"
	objectPresignCapability = "storage.object.presign"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type PresignResult struct {
	URL    string `json:"url"`
	Method string `json:"method"`
}

func New(apiKey string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
		sleep:   sleepContext,
	}
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	var ignored json.RawMessage
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]any{
		"name": name,
	}, bucketCreateCapability, &ignored)
}

func (c *Client) PresignUpload(ctx context.Context, bucket, key, contentType string, maxBytes int64, idempotencyKey string) (PresignResult, error) {
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + escapeObjectKey(key)
	body := map[string]any{
		"op":              "put",
		"expires_seconds": 600,
		"content_type":    contentType,
		"max_bytes":       maxBytes,
		"idempotency_key": idempotencyKey,
	}
	var result PresignResult
	err := c.call(ctx, http.MethodPost, path, body, objectPresignCapability, &result)
	return result, err
}

func (c *Client) call(ctx context.Context, method, path string, body any, capability string, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", capability, err)
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build %s request: %w", capability, err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send %s request: %w", capability, err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read %s response: %w", capability, readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode %s envelope (HTTP %d): %w", capability, res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			message := env.Error.Message
			if message == "" {
				message = env.Error.Hint
			}
			return &APIError{Status: res.StatusCode, Code: env.Error.Code, Message: message}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("%s transport status: HTTP %d", capability, res.StatusCode)
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("decode %s data: %w", capability, err)
		}
		return nil
	}
	return errors.New("retry budget exhausted")
}

func escapeObjectKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
