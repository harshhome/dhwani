package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"dhwani/internal/model"
)

func TestParseContentRangeTotal(t *testing.T) {
	total, err := parseContentRangeTotal("bytes 0-0/12345")
	if err != nil || total != 12345 {
		t.Fatalf("parseContentRangeTotal valid failed: total=%d err=%v", total, err)
	}
	if _, err := parseContentRangeTotal("bytes 0-0/*"); err == nil {
		t.Fatalf("expected unknown-total error")
	}
	if _, err := parseContentRangeTotal("bad"); err == nil {
		t.Fatalf("expected invalid content-range error")
	}
}

func TestProbeContentLengthFallsBackToRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// no Content-Length on HEAD branch
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-0/77")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("A"))
	}))
	defer srv.Close()

	s := &Server{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpClient: srv.Client(),
	}
	n, err := s.probeContentLength(context.Background(), srv.URL)
	if err != nil || n != 77 {
		t.Fatalf("probeContentLength fallback failed: n=%d err=%v", n, err)
	}
}

func TestCopyChunkRangeErrors(t *testing.T) {
	s := &Server{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpClient: &http.Client{},
	}
	if err := s.copyChunkRange(context.Background(), io.Discard, "http://example.test", -1, 0); err == nil {
		t.Fatalf("expected invalid chunk range error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", http.StatusBadGateway)
	}))
	defer srv.Close()
	s.httpClient = srv.Client()
	if err := s.copyChunkRange(context.Background(), io.Discard, srv.URL, 0, 0); err == nil {
		t.Fatalf("expected chunk status error")
	}
}

type errRT struct{}

func (errRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("network down") }

func TestStreamErrorBranches(t *testing.T) {
	// missing id
	{
		srv, store := newTestServer(t, nil, false)
		defer store.Close()
		req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&f=json", nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("missing-id stream expected 400 got %d", rr.Code)
		}
	}

	// invalid upstream stream URL
	{
		fp := &fakeProvider{streamRes: model.StreamResolution{Provider: "mx1", TrackProviderID: "x", ManifestMIME: "audio/flac", MediaURL: "://bad"}}
		srv, store := newTestServer(t, fp, false)
		defer store.Close()
		req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&f=json&id=x", nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadGateway {
			t.Fatalf("invalid-url stream expected 502 got %d", rr.Code)
		}
	}

	// upstream transport error
	{
		fp := &fakeProvider{streamRes: model.StreamResolution{Provider: "mx1", TrackProviderID: "x", ManifestMIME: "audio/flac", MediaURL: "http://upstream.test/audio"}}
		srv, store := newTestServer(t, fp, false)
		defer store.Close()
		srv.httpClient = &http.Client{Transport: errRT{}}
		req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&f=json&id=x", nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadGateway {
			t.Fatalf("transport-error stream expected 502 got %d", rr.Code)
		}
	}
}
