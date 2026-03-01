package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dhwani/internal/model"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS artist_metadata (
  provider TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  name TEXT,
  cover_art_url TEXT,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (provider, provider_id)
);

CREATE TABLE IF NOT EXISTS album_metadata (
  provider TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  artist_id TEXT,
  artist TEXT,
  name TEXT,
  year INTEGER,
  song_count INTEGER,
  duration_sec INTEGER,
  cover_art_url TEXT,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (provider, provider_id)
);

CREATE TABLE IF NOT EXISTS track_metadata (
  provider TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  title TEXT,
  artist TEXT,
  display_artist TEXT,
  album TEXT,
  genre TEXT,
  artist_id TEXT,
  album_id TEXT,
  duration_sec INTEGER,
  track_number INTEGER,
  disc_number INTEGER,
  bit_rate INTEGER,
  content_type TEXT,
  cover_art_url TEXT,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (provider, provider_id)
);

CREATE TABLE IF NOT EXISTS starred_items (
  item_type TEXT NOT NULL,
  provider TEXT NOT NULL,
  provider_item_id TEXT NOT NULL,
  starred_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (item_type, provider, provider_item_id)
);
`

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureSchemaColumns(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ensure schema columns: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error {
	if s == nil || s.DB == nil {
		return nil
	}
	return s.DB.Close()
}

func (s *Store) UpsertTrackMetadata(ctx context.Context, t model.Track) error {
	if s == nil || s.DB == nil || t.Provider == "" || t.ProviderID == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO track_metadata (
  provider, provider_id, title, artist, display_artist, album, genre, artist_id, album_id,
  duration_sec, track_number, disc_number, bit_rate, content_type, cover_art_url, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider, provider_id) DO UPDATE SET
  title = excluded.title,
  artist = excluded.artist,
  display_artist = excluded.display_artist,
  album = excluded.album,
  genre = excluded.genre,
  artist_id = excluded.artist_id,
  album_id = excluded.album_id,
  duration_sec = excluded.duration_sec,
  track_number = excluded.track_number,
  disc_number = excluded.disc_number,
  bit_rate = excluded.bit_rate,
  content_type = excluded.content_type,
  cover_art_url = excluded.cover_art_url,
  updated_at = CURRENT_TIMESTAMP
`, t.Provider, t.ProviderID, t.Title, t.Artist, t.DisplayArtist, t.Album, t.Genre, t.ArtistID, t.AlbumID, t.DurationSec, t.TrackNumber, t.DiscNumber, t.BitRate, t.ContentType, t.CoverArtURL)
	if err != nil {
		return err
	}
	_ = s.UpsertArtistMetadata(ctx, model.Artist{
		Provider:    t.Provider,
		ProviderID:  t.ArtistID,
		ID:          t.ArtistID,
		Name:        t.Artist,
		CoverArtURL: t.CoverArtURL,
	})
	return nil
}

func (s *Store) UpsertArtistMetadata(ctx context.Context, a model.Artist) error {
	if s == nil || s.DB == nil || strings.TrimSpace(a.Provider) == "" || strings.TrimSpace(a.ProviderID) == "" {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
INSERT INTO artist_metadata (provider, provider_id, name, cover_art_url, updated_at)
VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider, provider_id) DO UPDATE SET
  name = excluded.name,
  cover_art_url = excluded.cover_art_url,
  updated_at = CURRENT_TIMESTAMP
`, a.Provider, a.ProviderID, a.Name, a.CoverArtURL)
	return err
}

func (s *Store) UpsertAlbumMetadata(ctx context.Context, a model.Album) error {
	if s == nil || s.DB == nil || strings.TrimSpace(a.Provider) == "" || strings.TrimSpace(a.ProviderID) == "" {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	targetProvider := strings.TrimSpace(a.Provider)
	// Mirror providers can return identical raw album IDs. Reuse the existing row
	// for that provider_id instead of creating duplicates per provider name.
	var existingProvider string
	row := tx.QueryRowContext(ctx, `
SELECT provider
FROM album_metadata
WHERE provider_id = ?
ORDER BY updated_at DESC
LIMIT 1
`, strings.TrimSpace(a.ProviderID))
	if scanErr := row.Scan(&existingProvider); scanErr == nil && strings.TrimSpace(existingProvider) != "" {
		targetProvider = strings.TrimSpace(existingProvider)
	} else if scanErr != nil && scanErr != sql.ErrNoRows {
		err = scanErr
		return err
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO album_metadata (provider, provider_id, artist_id, artist, name, year, song_count, duration_sec, cover_art_url, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
ON CONFLICT(provider, provider_id) DO UPDATE SET
  artist_id = excluded.artist_id,
  artist = excluded.artist,
  name = excluded.name,
  year = CASE WHEN excluded.year > 0 THEN excluded.year ELSE album_metadata.year END,
  song_count = CASE WHEN excluded.song_count > 0 THEN excluded.song_count ELSE album_metadata.song_count END,
  duration_sec = CASE WHEN excluded.duration_sec > 0 THEN excluded.duration_sec ELSE album_metadata.duration_sec END,
  cover_art_url = excluded.cover_art_url,
  updated_at = CURRENT_TIMESTAMP
`, targetProvider, a.ProviderID, a.ArtistID, a.Artist, a.Name, a.Year, a.SongCount, a.DurationSec, a.CoverArtURL)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetAlbumMetadata(ctx context.Context, providerName, providerID string) (model.Album, error) {
	if s == nil || s.DB == nil {
		return model.Album{}, sql.ErrNoRows
	}
	row := s.DB.QueryRowContext(ctx, `
SELECT provider_id, provider, provider_id, artist_id, artist, name, song_count, duration_sec, year, cover_art_url
FROM album_metadata
WHERE provider = ? AND provider_id = ?
LIMIT 1
`, providerName, providerID)
	var a model.Album
	if err := row.Scan(
		&a.ID, &a.Provider, &a.ProviderID, &a.ArtistID, &a.Artist, &a.Name,
		&a.SongCount, &a.DurationSec, &a.Year, &a.CoverArtURL,
	); err != nil {
		return model.Album{}, err
	}
	return a, nil
