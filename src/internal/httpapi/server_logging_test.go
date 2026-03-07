package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactedQueryForLog_RedactsSubsonicCredentials(t *testing.T) {
	raw := "u=dhwani&p=change-me&t=abcdef&s=salty&c=curl&v=1.16.1&id=84097169"

	got := redactedQueryForLog(raw)

	for _, secret := range []string{"dhwani", "change-me", "abcdef", "salty"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted query leaked %q: %q", secret, got)
		}
	}
	for _, expected := range []string{"u=%5Bredacted%5D", "p=%5Bredacted%5D", "t=%5Bredacted%5D", "s=%5Bredacted%5D", "c=curl", "id=84097169"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("expected %q in redacted query: %q", expected, got)
		}
	}
}

func TestRedactedQueryForLog_BadQueryReturnsRedacted(t *testing.T) {
	if got := redactedQueryForLog("%zz"); got != "[redacted]" {
		t.Fatalf("expected [redacted], got %q", got)
	}
}

func TestSummarizeQueryForLog(t *testing.T) {
	raw := "u=secret&p=secret&c=android&v=1.16.1&id=123456789012345678901234567890&query=abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	got := summarizeQueryForLog(raw)
	if !strings.Contains(got, `c="android"`) || !strings.Contains(got, `v="1.16.1"`) {
		t.Fatalf("missing expected fields in summary: %q", got)
	}
	if !strings.Contains(got, `id="123456789012345678901234...`) {
		t.Fatalf("expected trimmed id in summary: %q", got)
	}
	if !strings.Contains(got, `query="abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMN...`) {
		t.Fatalf("expected trimmed query in summary: %q", got)
	}
}

func TestJsonFormatMiddlewareRewritesFParam(t *testing.T) {
	s := &Server{enableJSON: false}
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		if got := r.URL.Query().Get("f"); got != "" {
			t.Fatalf("expected f param to be removed, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/rest/ping.view?f=json&c=test", nil)
	rr := httptest.NewRecorder()
	s.jsonFormatMiddleware(next).ServeHTTP(rr, req)
	if !nextCalled {
		t.Fatalf("next handler not called")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
}
