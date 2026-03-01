package db

import (
	"context"
	"path/filepath"
	"testing"

	"dhwani/internal/model"
)

func TestStoreUpsertAndQueryFlows(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	t1 := model.Track{
		ID:          "t1",
		Provider:    "mx1",
		ProviderID:  "t1",
		Title:       "Track One",
		Artist:      "Artist One",
		Album:       "Album One",
		Genre:       "Pop",
		ArtistID:    "ar1",
		AlbumID:     "al1",
		DurationSec: 180,
		TrackNumber: 1,
		DiscNumber:  1,
		BitRate:     1411,
		ContentType: "audio/flac",
		CoverArtURL: "https://img/1",
	}
	t2 := model.Track{
		ID:          "t2",
		Provider:    "mx1",
		ProviderID:  "t2",
		Title:       "Track Two",
		Artist:      "Artist One",
		Album:       "Album One",
		Genre:       "Pop",
		ArtistID:    "ar1",
		AlbumID:     "al1",
		DurationSec: 200,
		TrackNumber: 2,
		DiscNumber:  1,
		BitRate:     1411,
		ContentType: "audio/flac",
		CoverArtURL: "https://img/2",
	}
	for _, tr := range []model.Track{t1, t2} {
		if err := store.UpsertTrackMetadata(ctx, tr); err != nil {
			t.Fatalf("UpsertTrackMetadata(%s) error = %v", tr.ID, err)
		}
	}

	gotByProvider, err := store.GetTrackMetadata(ctx, "mx1", "t1")
	if err != nil {
		t.Fatalf("GetTrackMetadata() error = %v", err)
	}
	if gotByProvider.Title != "Track One" || gotByProvider.ArtistID != "ar1" {
		t.Fatalf("unexpected track by provider: %#v", gotByProvider)
	}

	gotByID, err := store.GetTrackMetadataByID(ctx, "t2")
	if err != nil {
		t.Fatalf("GetTrackMetadataByID() error = %v", err)
	}
	if gotByID.Album != "Album One" {
		t.Fatalf("unexpected track by id: %#v", gotByID)
	}
	gotAnyByProviderID, err := store.GetAnyTrackMetadataByProviderID(ctx, "t2")
	if err != nil {
		t.Fatalf("GetAnyTrackMetadataByProviderID() error = %v", err)
	}
	if gotAnyByProviderID.ID != "t2" {
		t.Fatalf("unexpected any-track lookup: %#v", gotAnyByProviderID)
	}

	artists, err := store.ListCachedArtists(ctx, 10)
	if err != nil {
		t.Fatalf("ListCachedArtists() error = %v", err)
	}
	if len(artists) != 1 || artists[0].ID != "ar1" {
		t.Fatalf("unexpected artists: %#v", artists)
	}

	albums, err := store.ListCachedAlbums(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListCachedAlbums() error = %v", err)
	}
	if len(albums) != 1 || albums[0].SongCount != 2 {
		t.Fatalf("unexpected albums: %#v", albums)
	}
	albumsByArtist, err := store.ListCachedAlbumsByArtist(ctx, "ar1", 10, 0)
	if err != nil {
		t.Fatalf("ListCachedAlbumsByArtist() error = %v", err)
	}
	if len(albumsByArtist) != 1 {
		t.Fatalf("expected 1 album by artist, got %d", len(albumsByArtist))
	}
	cachedAlbum, err := store.GetCachedAlbum(ctx, "al1")
	if err != nil {
		t.Fatalf("GetCachedAlbum() error = %v", err)
	}
	if cachedAlbum.ID != "al1" {
		t.Fatalf("unexpected cached album: %#v", cachedAlbum)
	}
	cachedAlbumAny, err := store.GetCachedAlbumAny(ctx, "al1")
	if err != nil {
		t.Fatalf("GetCachedAlbumAny() error = %v", err)
	}
	if cachedAlbumAny.ID != "al1" {
		t.Fatalf("unexpected cached album any: %#v", cachedAlbumAny)
	}
	cachedArtist, err := store.GetCachedArtist(ctx, "ar1")
	if err != nil {
		t.Fatalf("GetCachedArtist() error = %v", err)
	}
	if cachedArtist.ID != "ar1" {
		t.Fatalf("unexpected cached artist: %#v", cachedArtist)
	}
	cachedArtistAny, err := store.GetCachedArtistAny(ctx, "ar1")
	if err != nil {
		t.Fatalf("GetCachedArtistAny() error = %v", err)
	}
	if cachedArtistAny.ID != "ar1" {
		t.Fatalf("unexpected cached artist any: %#v", cachedArtistAny)
	}

	tracks, err := store.ListCachedTracks(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListCachedTracks() error = %v", err)
	}
	if len(tracks) != 2 {
		t.Fatalf("expected 2 cached tracks, got %d", len(tracks))
	}

	tracksByAlbum, err := store.ListCachedTracksByAlbum(ctx, "al1", 10, 0)
	if err != nil {
		t.Fatalf("ListCachedTracksByAlbum() error = %v", err)
	}
	if len(tracksByAlbum) != 2 {
		t.Fatalf("expected 2 tracks by album, got %d", len(tracksByAlbum))
	}
	tracksByArtist, err := store.ListCachedTracksByArtist(ctx, "ar1", 10, 0)
	if err != nil {
		t.Fatalf("ListCachedTracksByArtist() error = %v", err)
	}
	if len(tracksByArtist) != 2 {
		t.Fatalf("expected 2 tracks by artist, got %d", len(tracksByArtist))
	}
	tracksByGenre, err := store.ListCachedTracksByGenre(ctx, "pop", 10, 0)
	if err != nil {
		t.Fatalf("ListCachedTracksByGenre() error = %v", err)
	}
	if len(tracksByGenre) != 2 {
		t.Fatalf("expected 2 tracks by genre, got %d", len(tracksByGenre))
	}
	randomTracks, err := store.RandomCachedTracks(ctx, 2)
	if err != nil {
		t.Fatalf("RandomCachedTracks() error = %v", err)
	}
	if len(randomTracks) != 2 {
		t.Fatalf("expected 2 random tracks, got %d", len(randomTracks))
	}

	genres, err := store.ListCachedGenres(ctx, 10)
	if err != nil {
		t.Fatalf("ListCachedGenres() error = %v", err)
	}
	if len(genres) != 1 || genres[0] != "Pop" {
		t.Fatalf("unexpected genres: %#v", genres)
	}

	if err := store.DeleteTrackByAnyID(ctx, "t1"); err != nil {
		t.Fatalf("DeleteTrackByAnyID() error = %v", err)
	}
	if _, err := store.GetTrackMetadataByID(ctx, "t1"); err == nil {
		t.Fatalf("expected deleted track to be missing")
	}
}

func TestAlbumMetadataDedupesAcrossProvidersByProviderID(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "dhwani.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	if err := store.UpsertAlbumMetadata(ctx, model.Album{
		Provider:    "mx1",
		ProviderID:  "84503043",
		ID:          "84503043",
		Name:        "Kati Patang",
		Artist:      "R. D. Burman",
		ArtistID:    "9097903",
		Year:        1970,
		SongCount:   18,
		DurationSec: 5208,
	}); err != nil {
		t.Fatalf("UpsertAlbumMetadata(mx1) error = %v", err)
	}
	if err := store.UpsertAlbumMetadata(ctx, model.Album{
		Provider:    "mx11",
		ProviderID:  "84503043",
		ID:          "84503043",
		Name:        "Kati Patang",
		Artist:      "R. D. Burman",
		ArtistID:    "9097903",
		Year:        1970,
		SongCount:   18,
		DurationSec: 5208,
	}); err != nil {
		t.Fatalf("UpsertAlbumMetadata(mx11) error = %v", err)
	}

	var count int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM album_metadata WHERE provider_id = ?`, "84503043").Scan(&count); err != nil {
		t.Fatalf("count album_metadata rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 album_metadata row for provider_id=84503043, got %d", count)
	}

	got, err := store.GetAlbumMetadataAny(ctx, "84503043")
	if err != nil {
		t.Fatalf("GetAlbumMetadataAny() error = %v", err)
	}
	if got.Artist != "R. D. Burman" || got.ArtistID != "9097903" {
		t.Fatalf("unexpected album metadata any: %#v", got)
	}
}
