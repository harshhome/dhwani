package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"dhwani/internal/db"
	"dhwani/internal/model"
	"dhwani/internal/provider"
)

type richProvider struct {
	name         string
	searchRes    model.SearchResult
	searchErr    error
	track        model.Track
	album        model.Album
	artist       model.Artist
	lyrics       model.Lyrics
	albumTracks  []model.Track
	artistAlbums []model.Album
	streamRes    model.StreamResolution
	streamErr    error
}

func (p *richProvider) Name() string { return p.name }
func (p *richProvider) Type() string { return "test" }
func (p *richProvider) Search(context.Context, string, int) (model.SearchResult, error) {
	return p.searchRes, p.searchErr
}
func (p *richProvider) GetArtist(context.Context, string) (model.Artist, error) {
	if p.artist.ID == "" {
		return model.Artist{}, provider.ErrNotFound
	}
	return p.artist, nil
}
func (p *richProvider) GetArtistAlbums(context.Context, string, int, int) ([]model.Album, error) {
	if len(p.artistAlbums) == 0 {
		return nil, provider.ErrNotFound
	}
	return p.artistAlbums, nil
}
func (p *richProvider) GetAlbum(context.Context, string) (model.Album, error) {
	if p.album.ID == "" {
		return model.Album{}, provider.ErrNotFound
	}
	return p.album, nil
}
func (p *richProvider) GetAlbumTracks(context.Context, string, int, int) ([]model.Track, error) {
	if len(p.albumTracks) == 0 {
		return nil, provider.ErrNotFound
	}
	return p.albumTracks, nil
}
func (p *richProvider) GetTrack(context.Context, string) (model.Track, error) {
	if p.track.ID == "" {
		return model.Track{}, provider.ErrNotFound
	}
	return p.track, nil
}
func (p *richProvider) GetLyrics(context.Context, string) (model.Lyrics, error) {
	if p.lyrics.Text == "" && len(p.lyrics.Lines) == 0 {
		return model.Lyrics{}, provider.ErrNotFound
	}
	return p.lyrics, nil
}
func (p *richProvider) ResolveStream(context.Context, string) (model.StreamResolution, error) {
	if p.streamErr != nil {
		return model.StreamResolution{}, p.streamErr
	}
	if p.streamRes.Provider == "" {
		return model.StreamResolution{}, provider.ErrNotFound
	}
	return p.streamRes, nil
}

func TestCatalogProviderBackedMethods(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	p := &richProvider{
		name: "mx1",
		searchRes: model.SearchResult{
			Tracks: []model.Track{{ID: "t1", Provider: "mx1", ProviderID: "t1", Title: "Song", Artist: "Artist", Album: "Album"}},
		},
		track: model.Track{
			ID:          "t1",
			Provider:    "mx1",
			ProviderID:  "t1",
			Title:       "Song",
			Artist:      "Artist",
			Album:       "Album",
			ArtistID:    "ar1",
			AlbumID:     "al1",
			ContentType: "audio/flac",
		},
		album: model.Album{
			ID:       "al1",
			Provider: "mx1",
			Name:     "Album",
			Artist:   "Artist",
			ArtistID: "ar1",
		},
		artist: model.Artist{
			ID:       "ar1",
			Provider: "mx1",
			Name:     "Artist",
		},
		lyrics: model.Lyrics{
			Artist: "Artist",
			Title:  "Song",
			Text:   "line one\nline two",
		},
		albumTracks: []model.Track{{
			ID:          "t1",
			Provider:    "mx1",
			ProviderID:  "t1",
			Title:       "Song",
			Artist:      "Artist",
			Album:       "Album",
			ArtistID:    "ar1",
			AlbumID:     "al1",
			ContentType: "audio/flac",
		}},
		artistAlbums: []model.Album{{
			ID:       "al1",
			Provider: "mx1",
			Name:     "Album",
			Artist:   "Artist",
			ArtistID: "ar1",
		}},
	}
	reg := provider.NewRegistry()
	if err := reg.Register(p); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	s := NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if _, err := s.Search(context.Background(), "q", 10); err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if _, err := s.GetTrack(context.Background(), "t1"); err != nil {
		t.Fatalf("GetTrack failed: %v", err)
	}
	if _, err := s.GetAlbum(context.Background(), "al1"); err != nil {
		t.Fatalf("GetAlbum failed: %v", err)
	}
	if _, err := s.GetArtist(context.Background(), "ar1"); err != nil {
		t.Fatalf("GetArtist failed: %v", err)
	}
	if albums, err := s.GetArtistAlbumsLive(context.Background(), "ar1", 10, 0); err != nil || len(albums) == 0 {
		t.Fatalf("GetArtistAlbumsLive failed: err=%v albums=%#v", err, albums)
	}
	if tracks, err := s.GetAlbumTracksLive(context.Background(), "al1", 10, 0); err != nil || len(tracks) == 0 {
		t.Fatalf("GetAlbumTracksLive failed: err=%v tracks=%#v", err, tracks)
	}
	if _, err := s.GetLyrics(context.Background(), "t1"); err != nil {
		t.Fatalf("GetLyrics failed: %v", err)
	}
}

func TestCatalogProbeAndDerivedArtists(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	reg := provider.NewRegistry()
	p := &richProvider{name: "mx1", searchRes: model.SearchResult{}}
	if err := reg.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}
	s := NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Covers probeProviders + stats ranking paths.
	s.probeProviders(context.Background())
	s.recordProviderResult("mx1", 120*time.Millisecond, true)
	s.recordProviderResult("mx1", 220*time.Millisecond, false)

	// Covers deriveArtistsFromTracks path.
	if err := store.UpsertTrackMetadata(context.Background(), model.Track{
		ID:         "t2",
		Provider:   "mx1",
		ProviderID: "t2",
		Title:      "Song Two",
		Artist:     "Derived Artist",
		Album:      "Derived Album",
		ArtistID:   "ar-derived",
		AlbumID:    "al-derived",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	artists, err := s.deriveArtistsFromTracks(context.Background(), 10)
	if err != nil || len(artists) == 0 {
		t.Fatalf("deriveArtistsFromTracks failed: err=%v artists=%#v", err, artists)
	}
}

func TestCatalogUnstarAndWarmIndexAndHydrate(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	p := &richProvider{
		name: "mx1",
		searchRes: model.SearchResult{
			Tracks: []model.Track{{
				ID:          "t-warm",
				Provider:    "mx1",
				ProviderID:  "t-warm",
				Title:       "Warm Song",
				Artist:      "Warm Artist",
				Album:       "Warm Album",
				ArtistID:    "ar-warm",
				AlbumID:     "al-warm",
				ContentType: "audio/flac",
			}},
		},
	}
	reg := provider.NewRegistry()
	if err := reg.Register(p); err != nil {
		t.Fatalf("register: %v", err)
	}
	s := NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// ensure warm index persists track mappings
	s.EnsureWarmIndex(context.Background(), "warm")
	if _, err := store.GetTrackMetadataByID(context.Background(), "t-warm"); err != nil {
		t.Fatalf("expected warmed track in cache, err=%v", err)
	}

	// hydrateTrackFromProviderSearch
	if tr, ok := s.hydrateTrackFromProviderSearch(context.Background(), "t-warm", "mx1", reg.Enabled()); !ok || tr.ID != "t-warm" {
		t.Fatalf("hydrateTrackFromProviderSearch failed: ok=%v track=%#v", ok, tr)
	}

	// unstar path should delete and evict recent
	s.RememberTracks([]model.Track{{ID: "t-warm", ProviderID: "t-warm", Title: "Warm Song"}})
	if err := s.UnstarTrack(context.Background(), "t-warm"); err != nil {
		t.Fatalf("UnstarTrack failed: %v", err)
	}
	if _, err := store.GetTrackMetadataByID(context.Background(), "t-warm"); err == nil {
		t.Fatalf("expected track to be deleted by unstar")
	}
	if _, ok := s.recentTrack("t-warm"); ok {
		t.Fatalf("expected recent cache eviction after unstar")
	}
}
