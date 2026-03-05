package service

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"dhwani/internal/db"
	"dhwani/internal/model"
	"dhwani/internal/provider"
)

func TestCatalogStoreWrappers(t *testing.T) {
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer store.Close()

	reg := provider.NewRegistry()
	s := NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	track := model.Track{
		ID:          "twrap",
		Provider:    "mx1",
		ProviderID:  "twrap",
		Title:       "Wrapper Song",
		Artist:      "Wrapper Artist",
		Album:       "Wrapper Album",
		Genre:       "Jazz",
		ArtistID:    "ar-wrap",
		AlbumID:     "al-wrap",
		ContentType: "audio/flac",
		CoverArtURL: "https://img/wrap.jpg",
	}
	if err := store.UpsertTrackMetadata(ctx, track); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if artists, err := s.ListArtists(ctx, 10); err != nil || len(artists) == 0 {
		t.Fatalf("ListArtists failed: err=%v artists=%#v", err, artists)
	}
	if albums, err := s.ListAlbums(ctx, 10, 0); err != nil || len(albums) == 0 {
		t.Fatalf("ListAlbums failed: err=%v albums=%#v", err, albums)
	}
	if albums, err := s.ListAlbumsByArtist(ctx, "ar-wrap", 10, 0); err != nil || len(albums) == 0 {
		t.Fatalf("ListAlbumsByArtist failed: err=%v albums=%#v", err, albums)
	}
	if tracks, err := s.ListTracks(ctx, 10, 0); err != nil || len(tracks) == 0 {
		t.Fatalf("ListTracks failed: err=%v tracks=%#v", err, tracks)
	}
	if tracks, err := s.ListTracksByAlbum(ctx, "al-wrap", 10, 0); err != nil || len(tracks) == 0 {
		t.Fatalf("ListTracksByAlbum failed: err=%v tracks=%#v", err, tracks)
	}
	if tracks, err := s.ListTracksByArtist(ctx, "ar-wrap", 10, 0); err != nil || len(tracks) == 0 {
		t.Fatalf("ListTracksByArtist failed: err=%v tracks=%#v", err, tracks)
	}
	if tracks, err := s.ListTracksByGenre(ctx, "Jazz", 10, 0); err != nil || len(tracks) == 0 {
		t.Fatalf("ListTracksByGenre failed: err=%v tracks=%#v", err, tracks)
	}
	if tracks, err := s.RandomTracks(ctx, 1); err != nil || len(tracks) != 1 {
		t.Fatalf("RandomTracks failed: err=%v tracks=%#v", err, tracks)
	}
	if genres, err := s.ListGenres(ctx, 10); err != nil || len(genres) == 0 {
		t.Fatalf("ListGenres failed: err=%v genres=%#v", err, genres)
	}

	if _, err := s.GetCachedAlbum(ctx, "al-wrap"); err != nil {
		t.Fatalf("GetCachedAlbum failed: %v", err)
	}
	if _, err := s.GetCachedAlbumAny(ctx, "al-wrap"); err != nil {
		t.Fatalf("GetCachedAlbumAny failed: %v", err)
	}
	if _, err := s.GetCachedArtist(ctx, "ar-wrap"); err != nil {
		t.Fatalf("GetCachedArtist failed: %v", err)
	}
	if _, err := s.GetCachedArtistAny(ctx, "ar-wrap"); err != nil {
		t.Fatalf("GetCachedArtistAny failed: %v", err)
	}
	if _, err := s.GetCachedTrackAny(ctx, "twrap"); err != nil {
		t.Fatalf("GetCachedTrackAny failed: %v", err)
	}

	if got := s.ResolveCoverArtURL(ctx, "twrap"); got == "" {
		t.Fatalf("ResolveCoverArtURL expected non-empty")
	}

	if err := s.PersistMappings(ctx, model.SearchResult{Tracks: []model.Track{track}}); err != nil {
		t.Fatalf("PersistMappings failed: %v", err)
	}

	// Should not panic and should return quickly.
	s.EnsureWarmIndex(ctx, "")
}
