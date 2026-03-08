package httpapi

import (
	"context"
	"errors"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"dhwani/internal/model"
)

func TestLyricsEndpoints(t *testing.T) {
	fp := &fakeProvider{
		searchRes: model.SearchResult{
			Tracks: []model.Track{{
				ID:       "t-lyric",
				Provider: "mx1", ProviderID: "t-lyric",
				Title: "My Song", Artist: "My Artist", Album: "My Album",
			}},
		},
		lyrics: model.Lyrics{
			Artist: "My Artist",
			Title:  "My Song",
			Lines: []model.LyricLine{
				{StartMs: 1000, Value: "line one"},
				{StartMs: 2000, Value: "line two"},
			},
		},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()

	req1 := httptest.NewRequest(http.MethodGet, "/rest/getLyrics.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t-lyric", nil)
	rr1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("getLyrics status=%d body=%s", rr1.Code, rr1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/rest/getLyricsBySongId.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t-lyric", nil)
	rr2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("getLyricsBySongId status=%d body=%s", rr2.Code, rr2.Body.String())
	}

	req3 := httptest.NewRequest(http.MethodGet, "/rest/getLyrics.view?u=u&p=p&v=1.16.1&c=test&f=json&artist=My%20Artist&title=My%20Song", nil)
	rr3 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("getLyrics by artist/title status=%d body=%s", rr3.Code, rr3.Body.String())
	}
}

func TestSearchErrorAndCanceledPaths(t *testing.T) {
	fpCanceled := &fakeProvider{searchErr: context.DeadlineExceeded}
	srvCanceled, store := newTestServer(t, fpCanceled, false)
	defer store.Close()

	reqCanceled := httptest.NewRequest(http.MethodGet, "/rest/search3.view?u=u&p=p&v=1.16.1&c=test&f=json&query=x&songCount=2", nil)
	rrCanceled := httptest.NewRecorder()
	srvCanceled.Router().ServeHTTP(rrCanceled, reqCanceled)
	if rrCanceled.Code != http.StatusOK {
		t.Fatalf("canceled search should return 200, got %d body=%s", rrCanceled.Code, rrCanceled.Body.String())
	}

	fpErr := &fakeProvider{searchErr: errors.New("boom")}
	srvErr, store2 := newTestServer(t, fpErr, false)
	defer store2.Close()
	reqErr := httptest.NewRequest(http.MethodGet, "/rest/search3.view?u=u&p=p&v=1.16.1&c=test&f=json&query=x&songCount=2", nil)
	rrErr := httptest.NewRecorder()
	srvErr.Router().ServeHTTP(rrErr, reqErr)
	if rrErr.Code != http.StatusBadGateway {
		t.Fatalf("error search should return 502, got %d body=%s", rrErr.Code, rrErr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rrErr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
}
