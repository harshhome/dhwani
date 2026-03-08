package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dhwani/internal/auth"
	"dhwani/internal/db"
	"dhwani/internal/model"
	"dhwani/internal/provider"
	"dhwani/internal/service"
)

func newTestServer(t *testing.T, fp *fakeProvider, ingestOnStar bool) (*Server, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	reg := provider.NewRegistry()
	if fp != nil {
		if err := reg.Register(fp); err != nil {
			t.Fatalf("register provider: %v", err)
		}
	}
	catalog := service.NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		ingestOnStar,
	)
	return srv, store
}

func TestSearch2ReturnsSearchResult2(t *testing.T) {
	fp := &fakeProvider{
		searchRes: model.SearchResult{
			Artists: []model.Artist{{ID: "ar1", Name: "Artist 1"}},
			Albums:  []model.Album{{ID: "al1", Name: "Album 1", Artist: "Artist 1", ArtistID: "ar1"}},
			Tracks:  []model.Track{{ID: "t1", Title: "Track 1", Artist: "Artist 1", Album: "Album 1", ArtistID: "ar1", AlbumID: "al1"}},
		},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/search2.view?u=u&p=p&v=1.16.1&c=test&f=json&query=track&artistCount=2&albumCount=2&songCount=2", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp := out["subsonic-response"].(map[string]any)
	if _, ok := resp["searchResult2"]; !ok {
		t.Fatalf("expected searchResult2 payload, got: %v", resp)
	}
}

func TestAlbumRandomGenreAndUnstarHandlers(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	ctx := context.Background()
	for _, tr := range []model.Track{
		{
			ID:          "t1",
			Provider:    "mx1",
			ProviderID:  "t1",
			Title:       "Song One",
			Artist:      "Artist A",
			Album:       "Album A",
			Genre:       "Pop",
			ArtistID:    "ar1",
			AlbumID:     "al1",
			ContentType: "audio/flac",
		},
		{
			ID:          "t2",
			Provider:    "mx1",
			ProviderID:  "t2",
			Title:       "Song Two",
			Artist:      "Artist A",
			Album:       "Album A",
			Genre:       "Pop",
			ArtistID:    "ar1",
			AlbumID:     "al1",
			ContentType: "audio/flac",
		},
	} {
		if err := store.UpsertTrackMetadata(ctx, tr); err != nil {
			t.Fatalf("seed track: %v", err)
		}
	}

	tests := []string{
		"/rest/getAlbumList2.view?u=u&p=p&v=1.16.1&c=test&f=json&size=10",
		"/rest/getRandomSongs.view?u=u&p=p&v=1.16.1&c=test&f=json&size=1",
		"/rest/getSongsByGenre.view?u=u&p=p&v=1.16.1&c=test&f=json&genre=Pop&size=10",
	}
	for _, path := range tests {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
	}

	unstarReq := httptest.NewRequest(http.MethodGet, "/rest/unstar.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t1", nil)
	unstarRR := httptest.NewRecorder()
	srv.Router().ServeHTTP(unstarRR, unstarReq)
	if unstarRR.Code != http.StatusOK {
		t.Fatalf("unstar status=%d body=%s", unstarRR.Code, unstarRR.Body.String())
	}
	if _, err := store.GetTrackMetadataByID(ctx, "t1"); err == nil {
		t.Fatalf("expected t1 to be removed by unstar")
	}
}

func TestStarHandlerRespondsOKWhenIngestDisabled(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/star.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t99", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("star status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSongArtistAlbumFallbackToCache(t *testing.T) {
	fp := &fakeProvider{
		trackErr:  provider.ErrNotFound,
		albumErr:  provider.ErrNotFound,
		artistErr: provider.ErrNotFound,
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	ctx := context.Background()

	seed := model.Track{
		ID:          "t-cache",
		Provider:    "mx1",
		ProviderID:  "t-cache",
		Title:       "Cached Song",
		Artist:      "Cached Artist",
		Album:       "Cached Album",
		Genre:       "Rock",
		ArtistID:    "ar-cache",
		AlbumID:     "al-cache",
		ContentType: "audio/flac",
		CoverArtURL: "https://img/cache.jpg",
	}
	if err := store.UpsertTrackMetadata(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, path := range []string{
		"/rest/getSong.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t-cache",
		"/rest/getArtist.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-cache",
		"/rest/getAlbum.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-cache",
		"/rest/getCoverArt.view?u=u&p=p&v=1.16.1&c=test&id=t-cache",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code >= 500 {
			t.Fatalf("%s status=%d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestGetSongUsesAlbumArtistFromCacheFirst(t *testing.T) {
	fp := &fakeProvider{
		track: model.Track{
			ID:          "84503055",
			Provider:    "mx1",
			ProviderID:  "84503055",
			Title:       "Yeh Jo Mohabbat Hai",
			Artist:      "Kishore Kumar",
			Album:       "Kati Patang (Original Motion Picture Soundtrack)",
			ArtistID:    "33057",
			AlbumID:     "84503043",
			ContentType: "audio/flac",
		},
		album: model.Album{
			ID:         "84503043",
			Provider:   "mx1",
			ProviderID: "84503043",
			Name:       "Kati Patang (Original Motion Picture Soundtrack)",
			Artist:     "R. D. Burman",
			ArtistID:   "9097903",
		},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	ctx := context.Background()

	// Cache-first behavior: persisted album metadata should win.
	if err := store.UpsertAlbumMetadata(ctx, model.Album{
		ID:         "84503043",
		Provider:   "mx1",
		ProviderID: "84503043",
		Name:       "Kati Patang (Original Motion Picture Soundtrack)",
		Artist:     "Kishore Kumar",
		ArtistID:   "33057",
	}); err != nil {
		t.Fatalf("seed stale album metadata: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/rest/getSong.view?u=u&p=p&v=1.16.1&c=test&f=json&id=84503055", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getSong status=%d body=%s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp, _ := out["subsonic-response"].(map[string]any)
	song, _ := resp["song"].(map[string]any)
	if got := song["displayAlbumArtist"]; got != "Kishore Kumar" {
		t.Fatalf("expected displayAlbumArtist=Kishore Kumar, got %#v", got)
	}
	albumArtists, _ := song["albumArtists"].([]any)
	if len(albumArtists) != 1 {
		t.Fatalf("expected one albumArtists entry, got %d", len(albumArtists))
	}
	aa, _ := albumArtists[0].(map[string]any)
	if got := aa["name"]; got != "Kishore Kumar" {
		t.Fatalf("expected albumArtists[0].name=Kishore Kumar, got %#v", got)
	}
	if got := aa["id"]; got != "ar-33057" {
		t.Fatalf("expected albumArtists[0].id=ar-33057, got %#v", got)
	}
	if got := fp.getAlbumCalls.Load(); got != 0 {
		t.Fatalf("expected no live album call when cache is present, got %d", got)
	}
}

