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

type ingestProvider struct {
	name         string
	tracks       map[string]model.Track
	albumTracks  map[string][]model.Track
	artistAlbums map[string][]model.Album
}

func (p *ingestProvider) Name() string { return p.name }
func (p *ingestProvider) Type() string { return "test" }
func (p *ingestProvider) Search(context.Context, string, int) (model.SearchResult, error) {
	return model.SearchResult{}, nil
}
func (p *ingestProvider) GetArtist(context.Context, string) (model.Artist, error) {
	return model.Artist{}, provider.ErrNotFound
}
func (p *ingestProvider) GetArtistAlbums(_ context.Context, artistProviderID string, _ int, _ int) ([]model.Album, error) {
	if out, ok := p.artistAlbums[artistProviderID]; ok {
		return out, nil
	}
	return nil, provider.ErrNotFound
}
func (p *ingestProvider) GetAlbum(context.Context, string) (model.Album, error) {
	return model.Album{}, provider.ErrNotFound
}
func (p *ingestProvider) GetAlbumTracks(_ context.Context, albumProviderID string, _ int, _ int) ([]model.Track, error) {
	if out, ok := p.albumTracks[albumProviderID]; ok {
		return out, nil
	}
	return nil, provider.ErrNotFound
}
func (p *ingestProvider) GetTrack(_ context.Context, trackProviderID string) (model.Track, error) {
	if tr, ok := p.tracks[trackProviderID]; ok {
		return tr, nil
	}
	return model.Track{}, provider.ErrNotFound
}
func (p *ingestProvider) GetLyrics(context.Context, string) (model.Lyrics, error) {
	return model.Lyrics{}, provider.ErrNotFound
}
func (p *ingestProvider) ResolveStream(context.Context, string) (model.StreamResolution, error) {
	return model.StreamResolution{}, provider.ErrNotFound
}

func newCatalogWithStore(t *testing.T, p provider.Provider) (*CatalogService, *db.Store) {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	reg := provider.NewRegistry()
	if err := reg.Register(p); err != nil {
		t.Fatalf("register provider: %v", err)
	}
	svc := NewCatalogService(reg, store, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return svc, store
}

func TestRecordPlayedTrackPersistsResolvedMetadata(t *testing.T) {
	p := &ingestProvider{
		name: "mx1",
		tracks: map[string]model.Track{
			"t1": {
				ID:          "t1",
				Provider:    "mx1",
				ProviderID:  "t1",
				Title:       "Song One",
				Artist:      "Artist One",
				Album:       "Album One",
				ArtistID:    "ar1",
				AlbumID:     "al1",
				ContentType: "audio/flac",
			},
		},
	}
	svc, store := newCatalogWithStore(t, p)
	defer store.Close()

	svc.RecordPlayedTrack(context.Background(), "t1")
	got, err := store.GetTrackMetadataByID(context.Background(), "t1")
	if err != nil {
		t.Fatalf("expected persisted played track, got err: %v", err)
	}
	if got.Title != "Song One" || got.ArtistID != "ar1" {
		t.Fatalf("unexpected stored track: %#v", got)
	}
}

func TestIngestStarredTrackAlbumArtist(t *testing.T) {
	p := &ingestProvider{
		name: "mx1",
		tracks: map[string]model.Track{
			"t2": {
				ID:          "t2",
				Provider:    "mx1",
				ProviderID:  "t2",
				Title:       "Song Two",
				Artist:      "Artist A",
				Album:       "Album A",
				ArtistID:    "arA",
				AlbumID:     "alA",
				ContentType: "audio/flac",
			},
		},
		albumTracks: map[string][]model.Track{
			"alA": {
				{
					ID:          "t2",
					Provider:    "mx1",
					ProviderID:  "t2",
					Title:       "Song Two",
					Artist:      "Artist A",
					Album:       "Album A",
					ArtistID:    "arA",
					AlbumID:     "alA",
					ContentType: "audio/flac",
				},
			},
			"alB": {
				{
					ID:          "t3",
					Provider:    "mx1",
					ProviderID:  "t3",
					Title:       "Song Three",
					Artist:      "Artist A",
					Album:       "Album B",
					ArtistID:    "arA",
					AlbumID:     "alB",
					ContentType: "audio/flac",
				},
			},
		},
		artistAlbums: map[string][]model.Album{
			"arA": {
				{ID: "alA", Provider: "mx1", ProviderID: "alA"},
				{ID: "alB", Provider: "mx1", ProviderID: "alB"},
			},
		},
	}
	svc, store := newCatalogWithStore(t, p)
	defer store.Close()

	if err := svc.IngestStarredTrack(context.Background(), "t2"); err != nil {
		t.Fatalf("IngestStarredTrack() error = %v", err)
	}
	if _, err := store.GetTrackMetadataByID(context.Background(), "t2"); err != nil {
		t.Fatalf("expected t2 persisted: %v", err)
	}

	if err := svc.IngestStarredAlbum(context.Background(), "alA"); err != nil {
		t.Fatalf("IngestStarredAlbum() error = %v", err)
	}
	if _, err := store.GetTrackMetadataByID(context.Background(), "t2"); err != nil {
		t.Fatalf("expected album-ingested track persisted: %v", err)
	}

	if err := svc.IngestStarredArtist(context.Background(), "arA"); err != nil {
		t.Fatalf("IngestStarredArtist() error = %v", err)
	}
	if _, err := store.GetTrackMetadataByID(context.Background(), "t3"); err != nil {
		t.Fatalf("expected artist-ingested track persisted: %v", err)
	}
}

func TestResolveCoverArtURLUsesRecentTrack(t *testing.T) {
	p := &ingestProvider{name: "mx1"}
	svc, store := newCatalogWithStore(t, p)
	defer store.Close()

	svc.RememberTracks([]model.Track{{
		ID:          "t9",
		Provider:    "mx1",
		ProviderID:  "t9",
		AlbumID:     "al9",
		ArtistID:    "ar9",
		CoverArtURL: "https://img/cover.jpg",
	}})
	if got := svc.ResolveCoverArtURL(context.Background(), "t9"); got != "https://img/cover.jpg" {
		t.Fatalf("expected recent cover art URL, got %q", got)
	}
}
