package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetchMonochromeInstancesFromLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	payload := `{"api":["https://hifi-one.spotisaver.net","https://hifi-one.spotisaver.net","http://arran.monochrome.tf"]}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := fetchMonochromeInstances(nil, path)
	if err != nil {
		t.Fatalf("fetch instances: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique instances, got %d (%v)", len(got), got)
	}
}

func TestFetchMonochromeInstancesFromFileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "instances.json")
	payload := `{"api":["https://hifi-one.spotisaver.net"]}`
	if err := os.WriteFile(path, []byte(payload), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, err := fetchMonochromeInstances(nil, "file://"+path)
	if err != nil {
		t.Fatalf("fetch instances: %v", err)
	}
	if len(got) != 1 || got[0] != "https://hifi-one.spotisaver.net" {
		t.Fatalf("unexpected instances: %v", got)
	}
}

func TestMergeInstancesDeduplicates(t *testing.T) {
	got := mergeInstances(
		[]string{"https://hifi-one.spotisaver.net", "http://arran.monochrome.tf"},
		[]string{"https://hifi-one.spotisaver.net/", "http://arran.monochrome.tf", "https://api.monochrome.tf"},
	)
	if len(got) != 3 {
		t.Fatalf("expected 3 unique instances, got %d (%v)", len(got), got)
	}
}

type roundTripFn func(*http.Request) (*http.Response, error)

func (f roundTripFn) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseLogLevelAndTimeoutHelpers(t *testing.T) {
	if got := parseLogLevel("debug"); got.String() != "DEBUG" {
		t.Fatalf("parseLogLevel(debug) = %s", got.String())
	}
	if got := parseLogLevel("warn"); got.String() != "WARN" {
		t.Fatalf("parseLogLevel(warn) = %s", got.String())
	}
	if got := parseLogLevel("error"); got.String() != "ERROR" {
		t.Fatalf("parseLogLevel(error) = %s", got.String())
	}
	if got := parseLogLevel("nope"); got.String() != "INFO" {
		t.Fatalf("parseLogLevel(default) = %s", got.String())
	}

	if got := timeoutForProvider(20, 0); got != 20*time.Second {
		t.Fatalf("timeoutForProvider default = %s", got)
	}
	if got := timeoutForProvider(20, 7); got != 7*time.Second {
		t.Fatalf("timeoutForProvider override = %s", got)
	}
	if got := timeoutForProvider(0, 0); got != 20*time.Second {
		t.Fatalf("timeoutForProvider fallback = %s", got)
	}
}

func TestUserAgentRoundTripper(t *testing.T) {
	called := false
	base := roundTripFn(func(r *http.Request) (*http.Response, error) {
		called = true
		if got := r.Header.Get("User-Agent"); got != "DhwaniTest/1.0" {
			t.Fatalf("expected user agent set, got %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	rt := userAgentRoundTripper{base: base, ua: "DhwaniTest/1.0"}
	req, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	if _, err := rt.RoundTrip(req); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if !called {
		t.Fatalf("base transport not called")
	}

	base2 := roundTripFn(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("User-Agent"); got != "CustomUA/2.0" {
			t.Fatalf("expected existing user agent preserved, got %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})
	rt2 := userAgentRoundTripper{base: base2, ua: "Ignored/1.0"}
	req2, _ := http.NewRequest(http.MethodGet, "https://example.test", nil)
	req2.Header.Set("User-Agent", "CustomUA/2.0")
	if _, err := rt2.RoundTrip(req2); err != nil {
		t.Fatalf("RoundTrip() with existing UA error = %v", err)
	}
}

func TestHTTPClientWithUserAgent(t *testing.T) {
	c := httpClientWithUserAgent(3*time.Second, "Test/1")
	if c.Timeout != 3*time.Second {
		t.Fatalf("unexpected client timeout: %s", c.Timeout)
	}
	if _, ok := c.Transport.(userAgentRoundTripper); !ok {
		t.Fatalf("expected userAgentRoundTripper transport")
	}
}

func TestFetchMonochromeInstancesFromHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("expected Accept header, got %q", got)
		}
		_, _ = io.WriteString(w, `{"api":["https://a.example","https://a.example/","https://b.example"]}`)
	}))
	defer srv.Close()

	got, err := fetchMonochromeInstances(nil, srv.URL)
	if err != nil {
		t.Fatalf("fetchMonochromeInstances(http) error = %v", err)
	}
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("unexpected instances: %#v", got)
	}
}

func TestFetchMonochromeInstancesHTTPFailures(t *testing.T) {
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer badStatus.Close()
	if _, err := fetchMonochromeInstances(nil, badStatus.URL); err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status error, got %v", err)
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"api":[`)
	}))
	defer badJSON.Close()
	if _, err := fetchMonochromeInstances(nil, badJSON.URL); err == nil {
		t.Fatalf("expected json decode error")
	}
}

func TestFetchMonochromeInstancesInputValidation(t *testing.T) {
	if _, err := fetchMonochromeInstances(nil, "   "); err == nil {
		t.Fatalf("expected empty source error")
	}
	if _, err := fetchMonochromeInstances(nil, "/path/does/not/exist/instances.json"); err == nil {
		t.Fatalf("expected file read error")
	}
}

func TestSourceAndPathAndNormalizeHelpers(t *testing.T) {
	if !isHTTPSource("https://example.com/x.json") {
		t.Fatalf("https should be detected as http source")
	}
	if isHTTPSource("file:///tmp/x.json") {
		t.Fatalf("file URL should not be detected as http source")
	}
	if isHTTPSource("not-a-url") {
		t.Fatalf("invalid URL should not be detected as http source")
	}

	if got := localInstancesPath("file:///tmp/instances.json"); got != "/tmp/instances.json" {
		t.Fatalf("unexpected file path conversion: %q", got)
	}
	if got := localInstancesPath("./a/../b/instances.json"); !strings.HasSuffix(got, "b/instances.json") {
		t.Fatalf("unexpected cleaned path: %q", got)
	}

	if got := normalizeBaseURL(" https://example.com/ "); got != "https://example.com" {
		t.Fatalf("unexpected normalized URL: %q", got)
	}
	if got := normalizeBaseURL("ht!tp://bad"); got != "" {
		t.Fatalf("expected empty normalized URL for invalid input, got %q", got)
	}
}
