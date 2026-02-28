package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhwani/internal/auth"
)

func TestUnknownRestEndpointReturnsNotFoundError(t *testing.T) {
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		auth.Credentials{Username: "u", Password: "p"},
		nil,
		true,
		false,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/doesNotExist.view?u=u&p=p&v=1.16.1&c=test", nil)
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `status="failed"`) || !strings.Contains(body, `message="endpoint not found"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestPingDoesNotRequireAuth(t *testing.T) {
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		auth.Credentials{Username: "u", Password: "p"},
		nil,
		true,
		false,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/ping.view?c=test&v=1.16.1&f=json", nil)
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestAuthStillRequiredForNonPing(t *testing.T) {
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil,
		auth.Credentials{Username: "u", Password: "p"},
		nil,
		true,
		false,
		false,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/getLicense.view?c=test&v=1.16.1&f=json", nil)
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rr.Code)
	}
}
