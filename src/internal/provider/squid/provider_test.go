package squid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhwani/internal/provider"
)

func TestDecodeManifest(t *testing.T) {
	raw := map[string]any{
		"mimeType":       "audio/flac",
		"codecs":         "flac",
		"encryptionType": "NONE",
		"urls":           []string{"https://example.test/file.flac"},
	}
	b, _ := json.Marshal(raw)
	encoded := base64.StdEncoding.EncodeToString(b)

	manifest, err := DecodeManifest(encoded)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if manifest.MIMEType != "audio/flac" || len(manifest.URLs) != 1 {
		t.Fatalf("unexpected manifest: %#v", manifest)
	}
}

func TestNormalizeTrackMap(t *testing.T) {
	in := map[string]any{
		"trackId":          "84097169",
		"title":            "Track One",
		"artist":           "Artist One",
		"album":            "Album One",
		"artistId":         "78910",
		"albumId":          "123456",
		"durationSeconds":  "211",
		"manifestMimeType": "audio/flac",
		"artists": []any{
			map[string]any{"id": "3yIf3UTEtZ2hXg0meUUxQu", "name": "Sukhwinder Singh"},
			map[string]any{"id": "04iAU9i5Mschjow8wJGFLZ", "name": "Shankar Mahadevan"},
			map[string]any{"id": "1vGg4tL6Hf8xQ2bKp2m8QH", "name": "Loy Mendonsa"},
		},
	}
	out := normalizeTrackMap("triton", in)
	if out.ID != "84097169" {
		t.Fatalf("unexpected id: %s", out.ID)
	}
	if out.AlbumID != "123456" || out.ArtistID != "78910" {
		t.Fatalf("unexpected relations: %#v", out)
	}
	if out.Title != "Track One" || out.ContentType != "audio/flac" {
		t.Fatalf("unexpected track normalization: %#v", out)
	}
	if out.DisplayArtist != "Sukhwinder Singh, Shankar Mahadevan & Loy Mendonsa" {
		t.Fatalf("unexpected display artist: %q", out.DisplayArtist)
	}
	if len(out.Artists) != 3 {
		t.Fatalf("unexpected artists count: %d", len(out.Artists))
	}
	if out.Artists[1].ID != "04iAU9i5Mschjow8wJGFLZ" || out.Artists[1].Name != "Shankar Mahadevan" {
		t.Fatalf("unexpected second artist: %#v", out.Artists[1])
	}
}

func TestResolveStreamNoFull(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"trackId":           1,
				"assetPresentation": "PREVIEW",
				"manifestMimeType":  "application/vnd.tidal.bts",
				"manifest":          base64.StdEncoding.EncodeToString([]byte(`{"mimeType":"audio/flac","urls":["https://x"]}`)),
			},
		})
	}))
	defer srv.Close()
	p, err := New(Config{Name: "mx1", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	_, err = p.ResolveStream(context.Background(), "1")
	if err == nil || !strings.Contains(err.Error(), provider.ErrNoFullStream.Error()) {
		t.Fatalf("expected no-full-stream error, got: %v", err)
	}
}

func TestResolveStreamDashManifest(t *testing.T) {
	xmlManifest := base64.StdEncoding.EncodeToString([]byte(`<MPD><Period><AdaptationSet><Representation><SegmentTemplate initialization="https://x/0.mp4" media="https://x/$Number$.mp4"><SegmentTimeline><S d="1"/></SegmentTimeline></SegmentTemplate></Representation></AdaptationSet></Period></MPD>`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"trackId":           1,
				"assetPresentation": "FULL",
				"manifestMimeType":  "application/dash+xml",
				"manifestHash":      "abc",
				"manifest":          xmlManifest,
			},
		})
	}))
	defer srv.Close()
	p, err := New(Config{Name: "mx1", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	res, err := p.ResolveStream(context.Background(), "1")
	if err != nil {
		t.Fatalf("resolve stream: %v", err)
	}
	if res.ManifestMIME != "application/dash+xml" || res.ManifestBase64 == "" || res.MediaURL != "" {
		t.Fatalf("unexpected resolution: %#v", res)
	}
}

func TestParseLRCSubtitles(t *testing.T) {
	in := "[00:01.23] first line\n[01:02.5] second line\ninvalid"
	lines := parseLRCSubtitles(in)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	if lines[0].StartMs != 1230 || lines[0].Value != "first line" {
		t.Fatalf("unexpected first line: %#v", lines[0])
	}
	if lines[1].StartMs != 62500 || lines[1].Value != "second line" {
		t.Fatalf("unexpected second line: %#v", lines[1])
	}
}

func TestIsRetryableProviderStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "not found sentinel", err: provider.ErrNotFound, want: true},
		{name: "400 status", err: providerStatusError{statusCode: http.StatusBadRequest}, want: true},
		{name: "wrapped 401 status", err: fmt.Errorf("wrapped: %w", providerStatusError{statusCode: http.StatusUnauthorized}), want: true},
		{name: "500 status", err: providerStatusError{statusCode: http.StatusInternalServerError}, want: false},
		{name: "generic error", err: fmt.Errorf("boom"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isRetryableProviderStatus(tc.err)
			if got != tc.want {
				t.Fatalf("isRetryableProviderStatus() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeAlbumMapUsesReleaseDateYear(t *testing.T) {
	alb := normalizeAlbumMap("mx1", map[string]any{
		"id":          "al1",
		"title":       "Album One",
		"artist":      "Artist One",
		"artistId":    "ar1",
		"releaseDate": "2019-07-04",
	})
	if alb.Year != 2019 {
		t.Fatalf("expected year 2019 from releaseDate, got %d", alb.Year)
	}
}

func TestNormalizeAlbumMapUsesNestedArtistObject(t *testing.T) {
	alb := normalizeAlbumMap("mx1", map[string]any{
		"id":    "84503043",
		"title": "Kati Patang (Original Motion Picture Soundtrack)",
		"artist": map[string]any{
			"id":   "9097903",
			"name": "R. D. Burman",
		},
	})
	if alb.Artist != "R. D. Burman" {
		t.Fatalf("expected artist from nested object, got %q", alb.Artist)
	}
	if alb.ArtistID != "9097903" {
		t.Fatalf("expected artist id from nested object, got %q", alb.ArtistID)
	}
}

func TestNormalizeAlbumMapUsesMainArtistFromArtistsList(t *testing.T) {
	alb := normalizeAlbumMap("mx1", map[string]any{
		"id":    "84503043",
		"title": "Kati Patang (Original Motion Picture Soundtrack)",
		"artists": []any{
			map[string]any{"id": "33057", "name": "Kishore Kumar", "type": "FEATURED"},
			map[string]any{"id": "9097903", "name": "R. D. Burman", "type": "MAIN"},
		},
	})
	if alb.Artist != "R. D. Burman" {
		t.Fatalf("expected MAIN artist from artists list, got %q", alb.Artist)
	}
	if alb.ArtistID != "9097903" {
		t.Fatalf("expected MAIN artist id from artists list, got %q", alb.ArtistID)
	}
}
