package subsonic

import (
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSuccessAndNewError(t *testing.T) {
	ok := NewSuccess(&PayloadUnion{
		Song: &Song{ID: "1", Title: "Track"},
	})
	if ok.Status != "ok" || ok.Version != APIVersion || ok.Song == nil || ok.Song.Title != "Track" {
		t.Fatalf("unexpected success response: %#v", ok)
	}

	fail := NewError(70, "boom")
	if fail.Status != "failed" || fail.Error == nil || fail.Error.Code != 70 {
		t.Fatalf("unexpected error response: %#v", fail)
	}
}

func TestWriteJSON(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rest/ping.view?f=json", nil)
	resp := NewSuccess(&PayloadUnion{Song: &Song{ID: "7", Title: "Seven"}})

	Write(rr, req, http.StatusAccepted, resp)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusAccepted)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("unexpected content type: %q", rr.Header().Get("Content-Type"))
	}

	var wrapped map[string]Response
	if err := json.Unmarshal(rr.Body.Bytes(), &wrapped); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}
	got, ok := wrapped["subsonic-response"]
	if !ok || got.Status != "ok" || got.Song == nil || got.Song.ID != "7" {
		t.Fatalf("unexpected json body: %#v", wrapped)
	}
}

func TestWriteXML(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/rest/ping.view", nil)
	resp := NewSuccess(&PayloadUnion{Song: &Song{ID: "9", Title: "Nine"}})

	Write(rr, req, http.StatusOK, resp)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Header().Get("Content-Type"), "application/xml") {
		t.Fatalf("unexpected content type: %q", rr.Header().Get("Content-Type"))
	}
	body := rr.Body.String()
	if !strings.HasPrefix(body, xml.Header) {
		t.Fatalf("xml header missing, body=%q", body)
	}
	if !strings.Contains(body, "subsonic-response") {
		t.Fatalf("xml root missing, body=%q", body)
	}
	if !strings.Contains(body, "song") || !strings.Contains(body, "Nine") {
		t.Fatalf("xml payload missing song data, body=%q", body)
	}
}
