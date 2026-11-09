package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestInitializeWebhookReusesCreatedWebhookAfterRestart(t *testing.T) {
	created := false
	creates := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bot token" {
			t.Error("missing bot authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		write := func(body string) {
			if _, err := io.WriteString(w, body); err != nil {
				t.Error(err)
			}
		}
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users/@me"):
			write(`{"id":"bot"}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/channels/channel/webhooks"):
			if created {
				write(`[{"id":"foreign","token":"other","user":{"id":"other"}},` +
					`{"id":"stable","token":"secret","user":{"id":"bot"}}]`)
			} else {
				write(`[{"id":"foreign","token":"other","user":{"id":"other"}},{"id":"unusable","user":{"id":"bot"}}]`)
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/channels/channel/webhooks"):
			var body struct {
				Name string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "Zefxi" {
				t.Errorf("create webhook name: %q, error: %v", body.Name, err)
			}
			creates++
			created = true
			write(`{"id":"stable","token":"secret"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		c, err := New("token", "guild", "channel", 1024)
		if err != nil {
			t.Fatal(err)
		}
		c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
			return http.DefaultTransport.RoundTrip(r)
		})}
		if err := c.InitializeWebhook(context.Background()); err != nil {
			t.Fatal(err)
		}
		if c.webhookID != "stable" || c.webhookToken != "secret" {
			t.Fatal("wrong webhook selected")
		}
	}
	if creates != 1 {
		t.Fatalf("created %d webhooks, want 1", creates)
	}
}

func TestInitializeWebhookErrorDoesNotLeakResponse(t *testing.T) {
	c, err := New("token", "guild", "channel", 1024)
	if err != nil {
		t.Fatal(err)
	}

	// REST error bodies can contain secrets and must not appear in the returned error.
	c.session.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden, Status: "403 Forbidden",
			Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`secret-webhook-token`)), Request: r,
		}, nil
	})}
	err = c.InitializeWebhook(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "secret-webhook-token") {
		t.Fatalf("unexpected error: %v", err)
	}
}
