package model

import "time"

type ProviderMeta struct {
	Name string
	Type string
}

type Artist struct {
	ID          string
	Provider    string
	ProviderID  string
	Name        string
	CoverArtURL string
}

type Album struct {
	ID          string
	Provider    string
	ProviderID  string
	ArtistID    string
	Artist      string
	Name        string
	SongCount   int
	DurationSec int
	Year        int
	CoverArtURL string
}

type Track struct {
	ID            string
	Provider      string
	ProviderID    string
	AlbumID       string
	ArtistID      string
	Title         string
	Artist        string
	DisplayArtist string
	Artists       []TrackArtist
	Album         string
	Genre         string
	DurationSec   int
	TrackNumber   int
	DiscNumber    int
	BitRate       int
	ContentType   string
	CoverArtURL   string
	ProviderRawID string
}

type TrackArtist struct {
	ID   string
	Name string
}

type SearchResult struct {
	Artists []Artist
	Albums  []Album
	Tracks  []Track
}

type Lyrics struct {
	Artist string
	Title  string
	Text   string
	Lines  []LyricLine
}

type LyricLine struct {
	StartMs int64
	Value   string
}

type StreamResolution struct {
	Provider          string
	TrackID           string
	TrackProviderID   string
	ManifestMIME      string
	ManifestBase64    string
	ManifestHash      string
	AssetPresentation string
	MediaURL          string
	ResolvedAt        time.Time
}
