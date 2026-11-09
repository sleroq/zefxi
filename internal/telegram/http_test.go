package telegram

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func TestRetryMultipartUpload(t *testing.T) {
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		bodies = append(bodies, body)
		if len(bodies) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"parameters":{"retry_after":1}}`)
			return
		}
		apiOK(t, w, `{"message_id":42}`)
	}))
	defer server.Close()
	b, err := bot.New("123:secret", bot.WithSkipGetMe(), bot.WithServerURL(server.URL),
		bot.WithHTTPClient(time.Minute, &retryClient{client: server.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	message, err := b.SendDocument(context.Background(), &bot.SendDocumentParams{
		ChatID: 1, Document: &models.InputFileUpload{Filename: "test.txt", Data: strings.NewReader("upload data")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.ID != 42 || len(bodies) != 3 || !bytes.Equal(bodies[0], bodies[1]) || !bytes.Equal(bodies[1], bodies[2]) || !bytes.Contains(bodies[0], []byte("upload data")) {
		t.Fatalf("message=%+v, request bodies=%q", message, bodies)
	}
}

func TestRetryCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"parameters":{"retry_after":60}}`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&retryClient{client: server.Client()}).Do(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v", err)
	}
}

func TestRetryPreservesInvalidResponse(t *testing.T) {
	const body = `{"ok":false,"description":"rate limited"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&retryClient{client: server.Client()}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Error(err)
		}
	}()
	got, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusTooManyRequests || string(got) != body {
		t.Fatalf("status=%d body=%q error=%v", resp.StatusCode, got, err)
	}
}

func TestRetryEmptyBody(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"parameters":{"retry_after":1}}`)
			return
		}
		apiOK(t, w, `{"id":1,"is_bot":true,"first_name":"bot"}`)
	}))
	defer server.Close()
	b, err := bot.New("123:secret", bot.WithSkipGetMe(), bot.WithServerURL(server.URL),
		bot.WithHTTPClient(time.Minute, &retryClient{client: server.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.GetMe(context.Background()); err != nil || calls != 2 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}
