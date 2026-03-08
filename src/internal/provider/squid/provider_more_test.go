package squid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dhwani/internal/model"
	"dhwani/internal/provider"
)

func TestProviderCoreEndpointsAndFallbacks(t *testing.T) {
	manifest := base64.StdEncoding.EncodeToString([]byte(`{"mimeType":"audio/flac","urls":["https://cdn.example/flac"]}`))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-client"); got != "DhwaniTest/1.0" {
			http.Error(w, "missing x-client", http.StatusBadRequest)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"artists": []map[string]any{{"id": "ar1", "name": "Artist One"}},
					"albums":  []map[string]any{{"id": "al1", "title": "Album One", "artistId": "ar1", "artist": "Artist One"}},
					"tracks": []map[string]any{
						{"id": "t1", "title": "Track One", "artist": "Artist One", "album": "Album One", "artistId": "ar1", "albumId": "al1"},
						{"id": "t2", "title": "Fallback Track", "artist": "Artist One", "album": "Fallback Album", "artistId": "ar1", "albumId": "al-fallback", "trackNumber": 1},
					},
				},
			})
		case "/artist":
			if q.Get("f") != "" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": map[string]any{
						"id":   "ar1",
						"name": "Artist One",
						"albums": map[string]any{
							"items": []map[string]any{
								{"id": "al1", "title": "Album One", "artistId": "ar1", "artist": "Artist One"},
							},
						},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": q.Get("id"), "name": "Artist One"}})
		case "/album":
			if q.Get("id") == "al-fallback" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":       "al1",
					"title":    "Album One",
					"artistId": "ar1",
					"artist":   "Artist One",
					"items": []map[string]any{
						{"type": "track", "id": "t1", "title": "Track One", "artistId": "ar1", "albumId": "al1", "trackNumber": 1},
					},
				},
			})
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": q.Get("id"), "title": "Track One", "artist": "Artist One", "album": "Album One", "artistId": "ar1", "albumId": "al1"}})
		case "/track":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":                q.Get("id"),
					"title":             "Track One",
					"artist":            "Artist One",
					"album":             "Album One",
					"artistId":          "ar1",
					"albumId":           "al1",
					"manifestMimeType":  "audio/flac",
					"assetPresentation": "FULL",
					"manifest":          manifest,
				},
			})
		case "/lyrics":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"artist":    "Artist One",
					"title":     "Track One",
					"subtitles": "[00:01.00] line one\n[00:02.00] line two",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(Config{
		Name:          "mx1",
		BaseURL:       srv.URL,
		ClientHeader:  "DhwaniTest/1.0",
		Source:        "tidal",
		StreamQuality: "LOSSLESS",
	}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	res, err := p.Search(context.Background(), "x", 10)
	if err != nil || len(res.Tracks) == 0 {
		t.Fatalf("Search() failed: err=%v res=%#v", err, res)
	}

	artist, err := p.GetArtist(context.Background(), "ar1")
	if err != nil || artist.ID != "ar1" {
		t.Fatalf("GetArtist() failed: err=%v artist=%#v", err, artist)
	}

	albums, err := p.GetArtistAlbums(context.Background(), "ar1", 10, 0)
	if err != nil || len(albums) != 1 {
		t.Fatalf("GetArtistAlbums() failed: err=%v albums=%#v", err, albums)
	}

	album, err := p.GetAlbum(context.Background(), "al1")
	if err != nil || album.ID != "al1" {
		t.Fatalf("GetAlbum() failed: err=%v album=%#v", err, album)
	}

	tracks, err := p.GetAlbumTracks(context.Background(), "al1", 10, 0)
	if err != nil || len(tracks) != 1 || tracks[0].AlbumID != "al1" {
		t.Fatalf("GetAlbumTracks() failed: err=%v tracks=%#v", err, tracks)
	}

	track, err := p.GetTrack(context.Background(), "t1")
	if err != nil || track.ID != "t1" || track.Artist != "Artist One" {
		t.Fatalf("GetTrack() failed: err=%v track=%#v", err, track)
	}

	lyrics, err := p.GetLyrics(context.Background(), "t1")
	if err != nil || !strings.Contains(lyrics.Text, "line one") || len(lyrics.Lines) != 2 {
		t.Fatalf("GetLyrics() failed: err=%v lyrics=%#v", err, lyrics)
	}

	stream, err := p.ResolveStream(context.Background(), "t1")
	if err != nil || stream.MediaURL == "" || stream.ManifestMIME != "audio/flac" {
		t.Fatalf("ResolveStream() failed: err=%v stream=%#v", err, stream)
	}

	// Album fallback path via /search when /album is missing.
	fbAlbum, err := p.GetAlbum(context.Background(), "al-fallback")
	if err != nil || fbAlbum.ID != "al-fallback" {
		t.Fatalf("GetAlbum fallback failed: err=%v album=%#v", err, fbAlbum)
	}
	fbTracks, err := p.GetAlbumTracks(context.Background(), "al-fallback", 10, 0)
	if err != nil || len(fbTracks) == 0 || fbTracks[0].AlbumID != "al-fallback" {
		t.Fatalf("GetAlbumTracks fallback failed: err=%v tracks=%#v", err, fbTracks)
	}
}

func TestSquidHelpers(t *testing.T) {
	if got := normalizeImageRef("123e4567-e89b-12d3-a456-426614174000"); !strings.Contains(got, "resources.tidal.com/images/") {
		t.Fatalf("normalizeImageRef tidal-id conversion failed: %q", got)
	}
	if got := windowTracks([]model.Track{{ID: "1"}, {ID: "2"}, {ID: "3"}}, 2, 1); len(got) != 2 || got[0].ID != "2" {
		t.Fatalf("windowTracks unexpected: %#v", got)
	}
	if got := getFirstMapFromArray(map[string]any{"x": []any{map[string]any{"id": "1"}}}, "x"); got["id"] != "1" {
		t.Fatalf("getFirstMapFromArray unexpected: %#v", got)
	}
	if got := formatDisplayArtistNames([]model.TrackArtist{{Name: "A"}, {Name: "B"}, {Name: "C"}}); got != "A, B & C" {
		t.Fatalf("formatDisplayArtistNames unexpected: %q", got)
	}
	if got := extractLyricsLines(map[string]any{
		"lines": []any{
			"line 1",
			map[string]any{"text": "line 2"},
		},
	}); len(got) != 2 || got[0] != "line 1" || got[1] != "line 2" {
		t.Fatalf("extractLyricsLines unexpected: %#v", got)
	}
}

func TestProviderNameTypeAndArtistAlbumsFallbackShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/artist":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"id":   "ar2",
					"name": "Artist Two",
					"items": []map[string]any{
						{"id": "al2", "title": "Album Two", "artistId": "ar2", "artist": "Artist Two"},
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New(Config{Name: "mx2", BaseURL: srv.URL}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if p.Name() != "mx2" || p.Type() != "squid" {
		t.Fatalf("unexpected Name/Type: %q %q", p.Name(), p.Type())
	}
	albums, err := p.GetArtistAlbums(context.Background(), "ar2", 10, 0)
	if err != nil || len(albums) != 1 || albums[0].ID != "al2" {
		t.Fatalf("GetArtistAlbums fallback-shape failed: err=%v albums=%#v", err, albums)
	}
}

func TestResolveStreamUsesContextPreferredQuality(t *testing.T) {
	manifest := base64.StdEncoding.EncodeToString([]byte(`{"mimeType":"audio/flac","urls":["https://cdn.example/flac"]}`))
	seenQuality := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/track" {
			http.NotFound(w, r)
			return
		}
		seenQuality = r.URL.Query().Get("quality")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":                "t1",
				"title":             "Track One",
				"artist":            "Artist One",
				"album":             "Album One",
				"artistId":          "ar1",
				"albumId":           "al1",
				"manifestMimeType":  "audio/flac",
				"assetPresentation": "FULL",
				"manifest":          manifest,
			},
		})
	}))
	defer srv.Close()

	p, err := New(Config{
		Name:          "mx1",
		BaseURL:       srv.URL,
		StreamQuality: "LOSSLESS",
	}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	ctx := provider.WithPreferredQuality(context.Background(), "HIGH")
	if _, err := p.ResolveStream(ctx, "t1"); err != nil {
		t.Fatalf("ResolveStream failed: %v", err)
	}
	if seenQuality != "HIGH" {
		t.Fatalf("expected quality HIGH, got %q", seenQuality)
	}
}

func TestResolveStreamStrictQualityDoesNotFallback(t *testing.T) {
	manifest := base64.StdEncoding.EncodeToString([]byte(`{"mimeType":"audio/flac","urls":["https://cdn.example/flac"]}`))
	seen := make([]string, 0, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/track" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("quality")
		seen = append(seen, q)
		if q == "HIGH" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":                "t1",
				"manifestMimeType":  "audio/flac",
				"assetPresentation": "FULL",
				"manifest":          manifest,
			},
		})
	}))
	defer srv.Close()

	p, err := New(Config{
		Name:          "mx1",
		BaseURL:       srv.URL,
		StreamQuality: "LOSSLESS",
	}, srv.Client())
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}

	ctx := provider.WithPreferredQuality(context.Background(), "HIGH")
	ctx = provider.WithStrictQuality(ctx, true)
	if _, err := p.ResolveStream(ctx, "t1"); err == nil {
		t.Fatalf("expected strict HIGH resolve to fail")
	}
	if len(seen) != 1 || seen[0] != "HIGH" {
		t.Fatalf("expected only HIGH attempt, got %#v", seen)
	}
}
