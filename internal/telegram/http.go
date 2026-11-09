package telegram

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

type retryClient struct {
	client *http.Client
}

func (c *retryClient) Do(req *http.Request) (*http.Response, error) {
	for {
		resp, err := c.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		delay := retryDelay(body)
		if delay <= 0 {
			resp.Body = struct {
				io.Reader
				io.Closer
			}{bytes.NewReader(body), resp.Body}
			return resp, nil
		}
		_ = resp.Body.Close()

		timer := time.NewTimer(delay)
		select {
		case <-req.Context().Done():
			timer.Stop()
			return nil, req.Context().Err()
		case <-timer.C:
		}

		req = req.Clone(req.Context())
		if req.Body != nil && req.Body != http.NoBody {
			req.Body, err = req.GetBody()
			if err != nil {
				return nil, err
			}
		}
	}
}

func retryDelay(body []byte) time.Duration {
	var result struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0
	}
	return time.Duration(result.Parameters.RetryAfter) * time.Second
}
