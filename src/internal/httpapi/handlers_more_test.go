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

func TestGetAlbumSongsUseAlbumArtist(t *testing.T) {
	fp := &fakeProvider{
		album: model.Album{
			ID:         "84097168",
			Provider:   "mx1",
			ProviderID: "84097168",
			Name:       "Aur Kaun? (Original Motion Picture Soundtrack)",
			Artist:     "Bappi Lahiri",
			ArtistID:   "3633746",
			Year:       1979,
		},
		albumTracks: []model.Track{
			{
				ID:          "84097169",
				Provider:    "mx1",
				ProviderID:  "84097169",
				Title:       "Haan Pahli Bar",
				Artist:      "Kishore Kumar",
				ArtistID:    "33057",
				Album:       "Aur Kaun? (Original Motion Picture Soundtrack)",
				AlbumID:     "84097168",
				TrackNumber: 1,
				ContentType: "audio/flac",
			},
			{
				ID:          "84097170",
				Provider:    "mx1",
				ProviderID:  "84097170",
				Title:       "Aur Kaun Aayega",
				Artist:      "Lata Mangeshkar",
				ArtistID:    "30815",
				Album:       "Aur Kaun? (Original Motion Picture Soundtrack)",
				AlbumID:     "84097168",
				TrackNumber: 2,
				ContentType: "audio/flac",
			},
		},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/getAlbum.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-84097168", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("getAlbum status=%d body=%s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp, _ := out["subsonic-response"].(map[string]any)
	album, _ := resp["album"].(map[string]any)
	songs, _ := album["song"].([]any)
	if len(songs) != 2 {
		t.Fatalf("expected 2 songs, got %d", len(songs))
	}
	for i, raw := range songs {
		song, _ := raw.(map[string]any)
		if got := song["displayAlbumArtist"]; got != "Bappi Lahiri" {
			t.Fatalf("song[%d] expected displayAlbumArtist=Bappi Lahiri, got %#v", i, got)
		}
		albumArtists, _ := song["albumArtists"].([]any)
		if len(albumArtists) != 1 {
			t.Fatalf("song[%d] expected 1 albumArtists entry, got %d", i, len(albumArtists))
		}
		aa, _ := albumArtists[0].(map[string]any)
		if got := aa["name"]; got != "Bappi Lahiri" {
			t.Fatalf("song[%d] expected albumArtists[0].name=Bappi Lahiri, got %#v", i, got)
		}
		if got := aa["id"]; got != "ar-3633746" {
			t.Fatalf("song[%d] expected albumArtists[0].id=ar-3633746, got %#v", i, got)
		}
	}
}

func TestStarHandlerIngestEnabled(t *testing.T) {
	fp := &fakeProvider{
		track: model.Track{
			ID:          "t-ingest",
			Provider:    "triton",
			ProviderID:  "t-ingest",
			Title:       "Ingest Song",
			Artist:      "Ingest Artist",
			Album:       "Ingest Album",
			ArtistID:    "ar-ingest",
			AlbumID:     "al-ingest",
			ContentType: "audio/flac",
		},
	}
	srv, store := newTestServer(t, fp, true)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/star.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t-ingest", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("star status=%d body=%s", rr.Code, rr.Body.String())
	}

	// async ingest path
	time.Sleep(120 * time.Millisecond)
	if _, err := store.GetTrackMetadataByID(context.Background(), "t-ingest"); err != nil {
		t.Fatalf("expected ingested track in store, err=%v", err)
	}
}

func TestSplitLyricsLinesAndPickLyricsTrackHelpers(t *testing.T) {
	lines := splitLyricsLines("  one \n\n two  \n")
	if len(lines) != 2 || lines[0] != "one" || lines[1] != "two" {
		t.Fatalf("unexpected splitLyricsLines output: %#v", lines)
	}
	tr := pickLyricsTrack([]model.Track{
		{ID: "1", Title: "Hello", Artist: "A"},
		{ID: "2", Title: "World", Artist: "B"},
	}, "B", "World")
	if tr.ID != "2" {
		t.Fatalf("pickLyricsTrack expected ID=2 got %#v", tr)
	}
}

func TestCoverArtPlaceholderOnUnknownID(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	req := httptest.NewRequest(http.MethodGet, "/rest/getCoverArt.view?u=u&p=p&v=1.16.1&c=test&id=does-not-exist", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("expected image/png placeholder, got %q", ct)
	}
}

func TestBadRequestBranchesForSongArtistAlbum(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	for _, path := range []string{
		"/rest/getSong.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getArtist.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getAlbum.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getLyricsBySongId.view?u=u&p=p&v=1.16.1&c=test&f=json",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s expected 400 got %d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}

func TestGenreAndRandomErrorBranchesWithClosedStore(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	_ = store.Close()
	req1 := httptest.NewRequest(http.MethodGet, "/rest/getRandomSongs.view?u=u&p=p&v=1.16.1&c=test&f=json&size=5", nil)
	rr1 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for closed-store random songs, got %d", rr1.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/rest/getSongsByGenre.view?u=u&p=p&v=1.16.1&c=test&f=json&genre=Rock&size=5", nil)
	rr2 := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for closed-store songsByGenre, got %d", rr2.Code)
	}
}

func TestNotFoundBranchesForSongArtistAlbum(t *testing.T) {
	fp := &fakeProvider{
		trackErr:  provider.ErrNotFound,
		albumErr:  provider.ErrNotFound,
		artistErr: provider.ErrNotFound,
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()

	for _, tc := range []struct {
		path string
		code int
	}{
		{path: "/rest/getSong.view?u=u&p=p&v=1.16.1&c=test&f=json&id=missing-track", code: http.StatusNotFound},
		{path: "/rest/getArtist.view?u=u&p=p&v=1.16.1&c=test&f=json&id=missing-artist", code: http.StatusNotFound},
		{path: "/rest/getAlbum.view?u=u&p=p&v=1.16.1&c=test&f=json&id=missing-album", code: http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != tc.code {
			t.Fatalf("%s expected %d got %d body=%s", tc.path, tc.code, rr.Code, rr.Body.String())
		}
	}
}

func TestStarHandlerClassifiesAlbumIDFromIDParam(t *testing.T) {
	fp := &fakeProvider{
		albumTracks: []model.Track{
			{
				ID:          "t-alb-1",
				Provider:    "triton",
				ProviderID:  "t-alb-1",
				Title:       "Album Song",
				Artist:      "Artist 1",
				Album:       "Album 1",
				ArtistID:    "ar-1",
				AlbumID:     "282205038",
				ContentType: "audio/flac",
			},
		},
	}
	srv, store := newTestServer(t, fp, true)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/star.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-282205038", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("star status=%d body=%s", rr.Code, rr.Body.String())
	}

	time.Sleep(150 * time.Millisecond)

	albums, err := store.ListStarredAlbums(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("list starred albums: %v", err)
	}
	if len(albums) != 1 || albums[0].ID != "282205038" {
		t.Fatalf("expected starred album 282205038, got %#v", albums)
	}
	if _, err := store.GetTrackMetadataByID(context.Background(), "t-alb-1"); err != nil {
		t.Fatalf("expected album track metadata ingested, err=%v", err)
	}
}

func TestStarHandlerArtistIDDoesNotIngestSongs(t *testing.T) {
	fp := &fakeProvider{
		artistAlbums: []model.Album{
			{
				ID:       "al-a1",
				Provider: "triton",
				Name:     "Artist Album",
			},
		},
		albumTracks: []model.Track{
			{
				ID:          "t-artist-1",
				Provider:    "triton",
				ProviderID:  "t-artist-1",
				Title:       "Artist Song",
				Artist:      "Artist 1",
				Album:       "Artist Album",
				ArtistID:    "123",
				AlbumID:     "al-a1",
				ContentType: "audio/flac",
			},
		},
	}
	srv, store := newTestServer(t, fp, true)
	defer store.Close()

	req := httptest.NewRequest(http.MethodGet, "/rest/star.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-123", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("star status=%d body=%s", rr.Code, rr.Body.String())
	}
	time.Sleep(150 * time.Millisecond)

	artists, err := store.ListStarredArtists(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("list starred artists: %v", err)
	}
	if len(artists) != 1 || artists[0].ID != "123" {
		t.Fatalf("expected starred artist 123, got %#v", artists)
	}
	tracks, err := store.ListCachedTracks(context.Background(), 20, 0)
	if err != nil {
		t.Fatalf("list cached tracks: %v", err)
	}
	if len(tracks) != 0 {
		t.Fatalf("expected no track ingestion when starring artist, got %d tracks", len(tracks))
	}
	if fp.getArtistAlbumsCalls.Load() != 0 || fp.getAlbumTracksCalls.Load() != 0 {
		t.Fatalf("expected no artist/album ingest calls, got artistAlbums=%d albumTracks=%d", fp.getArtistAlbumsCalls.Load(), fp.getAlbumTracksCalls.Load())
	}
}

func TestGetStarredIncludesArtistsAlbumsAndSongs(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	ctx := context.Background()

	if err := store.UpsertArtistMetadata(ctx, model.Artist{
		ID:         "901",
		Provider:   "triton",
		ProviderID: "901",
		Name:       "Star Artist",
	}); err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	if err := store.UpsertAlbumMetadata(ctx, model.Album{
		ID:         "801",
		Provider:   "triton",
		ProviderID: "801",
		Name:       "Star Album",
		Artist:     "Star Artist",
		ArtistID:   "901",
	}); err != nil {
		t.Fatalf("seed album: %v", err)
	}
	if err := store.UpsertTrackMetadata(ctx, model.Track{
		ID:          "701",
		Provider:    "triton",
		ProviderID:  "701",
		Title:       "Star Song",
		Artist:      "Star Artist",
		Album:       "Star Album",
		ArtistID:    "901",
		AlbumID:     "801",
		ContentType: "audio/flac",
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}
	if err := store.StarItem(ctx, "artist", "triton", "901"); err != nil {
		t.Fatalf("star artist: %v", err)
	}
	if err := store.StarItem(ctx, "album", "triton", "801"); err != nil {
		t.Fatalf("star album: %v", err)
	}
	if err := store.StarItem(ctx, "track", "triton", "701"); err != nil {
		t.Fatalf("star track: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/rest/getStarred.view?u=u&p=p&v=1.16.1&c=test&f=json", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp := out["subsonic-response"].(map[string]any)
	starred := resp["starred"].(map[string]any)
	if got := len(starred["artist"].([]any)); got != 1 {
		t.Fatalf("expected 1 starred artist, got %d", got)
	}
	if got := len(starred["album"].([]any)); got != 1 {
		t.Fatalf("expected 1 starred album, got %d", got)
	}
	if got := len(starred["song"].([]any)); got != 1 {
		t.Fatalf("expected 1 starred song, got %d", got)
	}
}

func TestParseDownloadQualities(t *testing.T) {
	got := parseDownloadQualities(" LOSSLESS, high,LOSSLESS , low ")
	if len(got) != 3 || got[0] != "LOSSLESS" || got[1] != "HIGH" || got[2] != "LOW" {
		t.Fatalf("unexpected qualities: %#v", got)
	}
}

func TestDownloadStarredTrackDASH(t *testing.T) {
	segments := map[string][]byte{
		"/init.mp4": []byte("INIT"),
		"/1.m4s":    []byte("AUDIO1"),
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := segments[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(data)))
			w.WriteHeader(http.StatusOK)
			return
		}
		if rg := r.Header.Get("Range"); rg != "" {
			raw := strings.TrimPrefix(rg, "bytes=")
			p := strings.SplitN(raw, "-", 2)
			start, _ := strconv.Atoi(p[0])
			end, _ := strconv.Atoi(p[1])
			if end >= len(data) {
				end = len(data) - 1
			}
			chunk := data[start : end+1]
			w.Header().Set("Content-Range", "bytes "+strconv.Itoa(start)+"-"+strconv.Itoa(end)+"/"+strconv.Itoa(len(data)))
			w.Header().Set("Content-Length", strconv.Itoa(len(chunk)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(chunk)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer upstream.Close()

	mpd := `<?xml version="1.0" encoding="UTF-8"?><MPD><Period><AdaptationSet mimeType="audio/mp4"><Representation id="r1"><SegmentTemplate initialization="` + upstream.URL + `/init.mp4" media="` + upstream.URL + `/$Number$.m4s" startNumber="1"><SegmentTimeline><S d="1"/></SegmentTimeline></SegmentTemplate></Representation></AdaptationSet></Period></MPD>`
	manifest := base64.StdEncoding.EncodeToString([]byte(mpd))

	fp := &fakeProvider{
		track: model.Track{
			ID:          "t-dash-dl",
			Provider:    "triton",
			ProviderID:  "t-dash-dl",
			Title:       "Dash Song",
			Artist:      "Dash Artist",
			Album:       "Dash Album",
			ArtistID:    "ar-dash",
			AlbumID:     "al-dash",
			ContentType: "audio/mp4",
		},
		streamRes: model.StreamResolution{
			Provider:        "triton",
			TrackProviderID: "t-dash-dl",
			ManifestMIME:    "application/dash+xml",
			ManifestBase64:  manifest,
			ManifestHash:    "h-dash",
		},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	srv.downloadOnStar = true
	srv.downloadDir = t.TempDir()

	if err := srv.downloadStarredTrack(context.Background(), "t-dash-dl"); err != nil {
		t.Fatalf("downloadStarredTrack dash failed: %v", err)
	}

	base := filepath.Join(srv.downloadDir, "Dash Artist", "Dash Album")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read download dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 downloaded file, got %d", len(entries))
	}
	got, err := os.ReadFile(filepath.Join(base, entries[0].Name()))
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != "INITAUDIO1" {
		t.Fatalf("unexpected downloaded content: %q", string(got))
	}
}

func TestDownloadStarredTrackWithRetryOnTransientHTTPStatus(t *testing.T) {
	attempts := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok-audio"))
	}))
	defer upstream.Close()

	fp := &fakeProvider{
		track: model.Track{
			ID:          "t-retry",
			Provider:    "triton",
			ProviderID:  "t-retry",
			Title:       "Retry Song",
			Artist:      "Retry Artist",
			Album:       "Retry Album",
			ArtistID:    "ar-r",
			AlbumID:     "al-r",
			ContentType: "audio/flac",
		},
		streamURL: upstream.URL,
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	srv.downloadOnStar = true
	srv.downloadDir = t.TempDir()
	srv.downloadRetryAttempts = 3

	if err := srv.downloadStarredTrackWithRetry(context.Background(), "t-retry"); err != nil {
		t.Fatalf("downloadStarredTrackWithRetry failed: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	base := filepath.Join(srv.downloadDir, "Retry Artist", "Retry Album")
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected one downloaded file, err=%v entries=%d", err, len(entries))
	}
}

func TestDownloadStarredTrackNoRetryWhenNoFullStream(t *testing.T) {
	fp := &fakeProvider{
		track: model.Track{
			ID:          "t-no-stream",
			Provider:    "triton",
			ProviderID:  "t-no-stream",
			Title:       "No Stream",
			Artist:      "Artist",
			Album:       "Album",
			ArtistID:    "ar-x",
			AlbumID:     "al-x",
			ContentType: "audio/flac",
		},
		streamErr: provider.ErrNoFullStream,
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	srv.downloadOnStar = true
	srv.downloadDir = t.TempDir()
	srv.downloadRetryAttempts = 5

	err := srv.downloadStarredTrackWithRetry(context.Background(), "t-no-stream")
	if err == nil {
		t.Fatalf("expected error for no full stream")
	}
	if fp.getTrackCalls.Load() != 1 {
		t.Fatalf("expected no retry when no stream; getTrack calls=%d", fp.getTrackCalls.Load())
	}
}

func TestUnstarClassifiesAlbumIDFromIDParam(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	ctx := context.Background()

	if err := store.UpsertAlbumMetadata(ctx, model.Album{
		ID:         "282205038",
		Provider:   "triton",
		ProviderID: "282205038",
		Name:       "Album To Unstar",
		Artist:     "Artist A",
		ArtistID:   "901",
	}); err != nil {
		t.Fatalf("seed album: %v", err)
	}
	if err := store.StarItem(ctx, "album", "triton", "282205038"); err != nil {
		t.Fatalf("star album: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/rest/unstar.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-282205038", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unstar status=%d body=%s", rr.Code, rr.Body.String())
	}

	albums, err := store.ListStarredAlbums(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list starred albums: %v", err)
	}
	if len(albums) != 0 {
		t.Fatalf("expected no starred albums after unstar, got %d", len(albums))
	}
	if _, err := store.GetCachedAlbum(ctx, "282205038"); err == nil {
		t.Fatalf("expected album metadata removed after unstar")
	}
}

func TestUnstarClassifiesArtistIDFromIDParam(t *testing.T) {
	srv, store := newTestServer(t, nil, false)
	defer store.Close()
	ctx := context.Background()

	if err := store.UpsertArtistMetadata(ctx, model.Artist{
		ID:         "123",
		Provider:   "triton",
		ProviderID: "123",
		Name:       "Artist To Unstar",
	}); err != nil {
		t.Fatalf("seed artist: %v", err)
	}
	if err := store.StarItem(ctx, "artist", "triton", "123"); err != nil {
		t.Fatalf("star artist: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/rest/unstar.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-123", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unstar status=%d body=%s", rr.Code, rr.Body.String())
	}

	artists, err := store.ListStarredArtists(ctx, 10, 0)
	if err != nil {
		t.Fatalf("list starred artists: %v", err)
	}
	if len(artists) != 0 {
		t.Fatalf("expected no starred artists after unstar, got %d", len(artists))
	}
	if _, err := store.GetCachedArtist(ctx, "123"); err == nil {
		t.Fatalf("expected artist metadata removed after unstar")
	}
}
