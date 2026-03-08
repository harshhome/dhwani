package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dhwani/internal/auth"
	"dhwani/internal/model"
	"dhwani/internal/provider"
	"dhwani/internal/service"
)

type fakeProvider struct {
	streamURL string
	streamRes model.StreamResolution
	streamErr error
	searchRes model.SearchResult
	searchErr error
	lyrics    model.Lyrics
	lyricsErr error
	track     model.Track
	trackErr  error
	album     model.Album
	albumErr  error
	albumTracks    []model.Track
	albumTracksErr error
	artist    model.Artist
	artistErr error
	artistAlbums    []model.Album
	artistAlbumsErr error

	getTrackCalls        atomic.Int32
	getAlbumCalls        atomic.Int32
	getArtistCalls       atomic.Int32
	getAlbumTracksCalls  atomic.Int32
	getArtistAlbumsCalls atomic.Int32
}

func (f *fakeProvider) Name() string { return "triton" }
func (f *fakeProvider) Type() string { return "squid" }
func (f *fakeProvider) Search(ctx context.Context, query string, limit int) (model.SearchResult, error) {
	if f.searchErr != nil {
		return model.SearchResult{}, f.searchErr
	}
	return f.searchRes, nil
}
func (f *fakeProvider) GetArtist(ctx context.Context, artistProviderID string) (model.Artist, error) {
	f.getArtistCalls.Add(1)
	if f.artistErr != nil {
		return model.Artist{}, f.artistErr
	}
	if strings.TrimSpace(f.artist.ID) != "" {
		return f.artist, nil
	}
	return model.Artist{}, provider.ErrNotFound
}
func (f *fakeProvider) GetArtistAlbums(ctx context.Context, artistProviderID string, limit int, offset int) ([]model.Album, error) {
	f.getArtistAlbumsCalls.Add(1)
	if f.artistAlbumsErr != nil {
		return nil, f.artistAlbumsErr
	}
	if f.artistAlbums != nil {
		return f.artistAlbums, nil
	}
	return []model.Album{}, nil
}
func (f *fakeProvider) GetAlbum(ctx context.Context, albumProviderID string) (model.Album, error) {
	f.getAlbumCalls.Add(1)
	if f.albumErr != nil {
		return model.Album{}, f.albumErr
	}
	if strings.TrimSpace(f.album.ID) != "" {
		return f.album, nil
	}
	return model.Album{}, provider.ErrNotFound
}
func (f *fakeProvider) GetAlbumTracks(ctx context.Context, albumProviderID string, limit int, offset int) ([]model.Track, error) {
	f.getAlbumTracksCalls.Add(1)
	if f.albumTracksErr != nil {
		return nil, f.albumTracksErr
	}
	if f.albumTracks != nil {
		return f.albumTracks, nil
	}
	return []model.Track{}, nil
}
func (f *fakeProvider) GetTrack(ctx context.Context, trackProviderID string) (model.Track, error) {
	f.getTrackCalls.Add(1)
	if f.trackErr != nil {
		return model.Track{}, f.trackErr
	}
	if strings.TrimSpace(f.track.ID) != "" {
		return f.track, nil
	}
	return model.Track{ID: "84097169", ProviderID: "84097169"}, nil
}
func (f *fakeProvider) GetLyrics(ctx context.Context, trackProviderID string) (model.Lyrics, error) {
	if f.lyricsErr != nil {
		return model.Lyrics{}, f.lyricsErr
	}
	if f.lyrics.Text != "" || len(f.lyrics.Lines) > 0 {
		return f.lyrics, nil
	}
	return model.Lyrics{}, provider.ErrNotFound
}
func (f *fakeProvider) ResolveStream(ctx context.Context, trackProviderID string) (model.StreamResolution, error) {
	if f.streamErr != nil {
		return model.StreamResolution{}, f.streamErr
	}
	if f.streamRes.Provider != "" {
		return f.streamRes, nil
	}
	return model.StreamResolution{Provider: "triton", TrackProviderID: trackProviderID, MediaURL: f.streamURL, ManifestMIME: "audio/flac"}, nil
}

func TestStreamRangePassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-3" {
			t.Fatalf("expected range header to be forwarded, got %q", got)
		}
		w.Header().Set("Content-Type", "audio/flac")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-3/10")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("ABCD"))
	}))
	defer upstream.Close()

	reg := provider.NewRegistry()
	if err := reg.Register(&fakeProvider{streamURL: upstream.URL}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))

	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&id=84097169", nil)
	req.Header.Set("Range", "bytes=0-3")
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusPartialContent {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Range"); got != "bytes 0-3/10" {
		t.Fatalf("expected content-range passthrough, got %q", got)
	}
	if body := rr.Body.String(); body != "ABCD" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestStreamDashRangePassthrough(t *testing.T) {
	segments := map[string][]byte{
		"/init.mp4": []byte("INIT"),
		"/1.mp4":    []byte("ABCDE"),
		"/2.mp4":    []byte("FGHIJ"),
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

	mpd := `<?xml version="1.0" encoding="UTF-8"?><MPD><Period><AdaptationSet mimeType="audio/mp4"><Representation id="r1"><SegmentTemplate initialization="` + upstream.URL + `/init.mp4" media="` + upstream.URL + `/$Number$.mp4" startNumber="1"><SegmentTimeline><S d="1" r="1"/></SegmentTimeline></SegmentTemplate></Representation></AdaptationSet></Period></MPD>`
	manifest := base64.StdEncoding.EncodeToString([]byte(mpd))

	reg := provider.NewRegistry()
	if err := reg.Register(&fakeProvider{
		streamRes: model.StreamResolution{
			Provider:        "triton",
			TrackProviderID: "84097169",
			ManifestMIME:    "application/dash+xml",
			ManifestBase64:  manifest,
			ManifestHash:    "h1",
		},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&id=84097169", nil)
	req.Header.Set("Range", "bytes=4-8")
	rr := httptest.NewRecorder()

	srv.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusPartialContent {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Range"); got != "bytes 4-8/14" {
		t.Fatalf("unexpected content-range: %q", got)
	}
	if body := rr.Body.String(); body != "ABCDE" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestStreamNoFullReturnsSubsonicError(t *testing.T) {
	reg := provider.NewRegistry()
	if err := reg.Register(&fakeProvider{streamErr: provider.ErrNoFullStream}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)
	req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&f=json&id=84097169", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp := out["subsonic-response"].(map[string]any)
	errObj := resp["error"].(map[string]any)
	if code := int(errObj["code"].(float64)); code != 70 {
		t.Fatalf("unexpected error code: %d", code)
	}
	if msg := errObj["message"].(string); !strings.Contains(msg, "FULL") {
		t.Fatalf("unexpected error message: %s", msg)
	}
}

func TestStreamDashInvalidRangeReturns416(t *testing.T) {
	segments := map[string][]byte{
		"/init.mp4": []byte("INIT"),
		"/1.mp4":    []byte("ABCDE"),
		"/2.mp4":    []byte("FGHIJ"),
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
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}))
	defer upstream.Close()

	mpd := `<?xml version="1.0" encoding="UTF-8"?><MPD><Period><AdaptationSet mimeType="audio/mp4"><Representation id="r1"><SegmentTemplate initialization="` + upstream.URL + `/init.mp4" media="` + upstream.URL + `/$Number$.mp4" startNumber="1"><SegmentTimeline><S d="1" r="1"/></SegmentTimeline></SegmentTemplate></Representation></AdaptationSet></Period></MPD>`
	manifest := base64.StdEncoding.EncodeToString([]byte(mpd))

	reg := provider.NewRegistry()
	if err := reg.Register(&fakeProvider{
		streamRes: model.StreamResolution{
			Provider:        "triton",
			TrackProviderID: "84097169",
			ManifestMIME:    "application/dash+xml",
			ManifestBase64:  manifest,
			ManifestHash:    "h1",
		},
	}); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/stream.view?u=u&p=p&v=1.16.1&c=test&f=json&id=84097169", nil)
	req.Header.Set("Range", "bytes=99-120")
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("json decode: %v", err)
	}
	resp := out["subsonic-response"].(map[string]any)
	errObj := resp["error"].(map[string]any)
	if code := int(errObj["code"].(float64)); code != 10 {
		t.Fatalf("unexpected error code: %d", code)
	}
}

func TestGetCoverArtArtistPrefixOnlyUsesArtistLookup(t *testing.T) {
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ARTIST-IMG"))
	}))
	defer img.Close()

	fp := &fakeProvider{
		artist: model.Artist{ID: "123", CoverArtURL: img.URL},
		track:  model.Track{ID: "123", CoverArtURL: img.URL + "/track"},
		album:  model.Album{ID: "123", CoverArtURL: img.URL + "/album"},
	}
	reg := provider.NewRegistry()
	if err := reg.Register(fp); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/getCoverArt.view?u=u&p=p&v=1.16.1&c=test&id=ar-123", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if body := rr.Body.String(); body != "ARTIST-IMG" {
		t.Fatalf("unexpected body: %q", body)
	}
	if got := fp.getArtistCalls.Load(); got == 0 {
		t.Fatalf("expected artist lookup")
	}
	if got := fp.getAlbumCalls.Load(); got != 0 {
		t.Fatalf("expected no album lookups, got %d", got)
	}
	if got := fp.getTrackCalls.Load(); got != 0 {
		t.Fatalf("expected no track lookups, got %d", got)
	}
}

func TestGetCoverArtAlbumPrefixOnlyUsesAlbumLookup(t *testing.T) {
	img := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ALBUM-IMG"))
	}))
	defer img.Close()

	fp := &fakeProvider{
		album:  model.Album{ID: "456", CoverArtURL: img.URL},
		track:  model.Track{ID: "456", CoverArtURL: img.URL + "/track"},
		artist: model.Artist{ID: "456", CoverArtURL: img.URL + "/artist"},
	}
	reg := provider.NewRegistry()
	if err := reg.Register(fp); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	catalog := service.NewCatalogService(reg, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		catalog,
		auth.Credentials{Username: "u", Password: "p"},
		&http.Client{Timeout: 5 * time.Second},
		true,
		false,
		true,
	)

	req := httptest.NewRequest(http.MethodGet, "/rest/getCoverArt.view?u=u&p=p&v=1.16.1&c=test&id=al-456", nil)
	rr := httptest.NewRecorder()
	srv.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rr.Code)
	}
	if body := rr.Body.String(); body != "ALBUM-IMG" {
		t.Fatalf("unexpected body: %q", body)
	}
	if got := fp.getAlbumCalls.Load(); got == 0 {
		t.Fatalf("expected album lookup")
	}
	if got := fp.getArtistCalls.Load(); got != 0 {
		t.Fatalf("expected no artist lookups, got %d", got)
	}
	if got := fp.getTrackCalls.Load(); got != 0 {
		t.Fatalf("expected no track lookups, got %d", got)
	}
}

func TestCoverArtIDHelpers(t *testing.T) {
	srv := &Server{}
	if got := srv.artistCoverArtID("123"); got != "ar-123" {
		t.Fatalf("unexpected artist cover id: %q", got)
	}
	if got := srv.albumCoverArtID("456"); got != "al-456" {
		t.Fatalf("unexpected album cover id: %q", got)
	}
	if got := srv.trackCoverArtID("789"); got != "789" {
		t.Fatalf("unexpected track cover id: %q", got)
	}

	kind, raw := parseCoverArtID("ar-123")
	if kind != "artist" || raw != "123" {
		t.Fatalf("unexpected parse for artist: kind=%q raw=%q", kind, raw)
	}
	kind, raw = parseCoverArtID("al-456")
	if kind != "album" || raw != "456" {
		t.Fatalf("unexpected parse for album: kind=%q raw=%q", kind, raw)
	}
	kind, raw = parseCoverArtID("789")
	if kind != "track" || raw != "789" {
		t.Fatalf("unexpected parse for track: kind=%q raw=%q", kind, raw)
	}
}
