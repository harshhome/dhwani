package squid

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhwani/internal/model"
	"dhwani/internal/provider"
)

type Config struct {
	Name          string
	BaseURL       string
	ClientHeader  string
	Source        string
	StreamQuality string
}

type Provider struct {
	cfg    Config
	client *http.Client
}

type providerStatusError struct {
	statusCode int
}

func (e providerStatusError) Error() string {
	return fmt.Sprintf("provider status %d", e.statusCode)
}

func New(cfg Config, client *http.Client) (*Provider, error) {
	if cfg.Name == "" || cfg.BaseURL == "" {
		return nil, fmt.Errorf("squid provider name and base_url are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	return &Provider{cfg: cfg, client: client}, nil
}

func (p *Provider) Name() string { return p.cfg.Name }
func (p *Provider) Type() string { return "squid" }

func (p *Provider) Search(ctx context.Context, query string, limit int) (model.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.SearchResult{}, err
	}
	u.Path = joinPath(u.Path, "search")
	q := u.Query()
	q.Set("s", query)
	q.Set("limit", strconv.Itoa(limit))
	q.Set("a", "1")
	q.Set("al", "1")
	q.Set("v", "0")
	q.Set("p", "0")
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		return model.SearchResult{}, err
	}
	return normalizeSearch(p.cfg.Name, body), nil
}

func (p *Provider) GetArtist(ctx context.Context, artistProviderID string) (model.Artist, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.Artist{}, err
	}
	u.Path = joinPath(u.Path, "artist")
	q := u.Query()
	q.Set("id", artistProviderID)
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		return model.Artist{}, err
	}
	artist := normalizeArtistMap(p.cfg.Name, body)
	if artist.ID == "" {
		return model.Artist{}, provider.ErrNotFound
	}
	return artist, nil
}

func (p *Provider) GetArtistAlbums(ctx context.Context, artistProviderID string, limit int, offset int) ([]model.Album, error) {
	if limit <= 0 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = joinPath(u.Path, "artist")
	q := u.Query()
	// Squid/Tidal-compatible endpoint for artist albums.
	q.Set("f", artistProviderID)
	q.Set("skip_tracks", "true")
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		return nil, err
	}

	artist := normalizeArtistMap(p.cfg.Name, body)
	albums := make([]model.Album, 0)
	if am := getMap(body, "albums"); am != nil {
		for _, item := range extractList(am, "items", "albums", "album") {
			alb := normalizeAlbumMap(p.cfg.Name, item)
			if alb.ID == "" {
				continue
			}
			if alb.ArtistID == "" {
				alb.ArtistID = artist.ID
			}
			if alb.Artist == "" {
				alb.Artist = artist.Name
			}
			albums = append(albums, alb)
		}
	}
	if len(albums) == 0 {
		for _, item := range extractList(body, "items", "albums", "album") {
			alb := normalizeAlbumMap(p.cfg.Name, item)
			if alb.ID == "" {
				continue
			}
			if alb.ArtistID == "" {
				alb.ArtistID = artist.ID
			}
			if alb.Artist == "" {
				alb.Artist = artist.Name
			}
			albums = append(albums, alb)
		}
	}

	if offset >= len(albums) {
		return []model.Album{}, nil
	}
	end := offset + limit
	if end > len(albums) {
		end = len(albums)
	}
	return albums[offset:end], nil
}

func (p *Provider) GetAlbum(ctx context.Context, albumProviderID string) (model.Album, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.Album{}, err
	}
	u.Path = joinPath(u.Path, "album")
	q := u.Query()
	q.Set("id", albumProviderID)
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		alb, _, fbErr := p.searchAlbumFallback(ctx, albumProviderID)
		if fbErr == nil && alb.ID != "" {
			return alb, nil
		}
		return model.Album{}, err
	}
	album := normalizeAlbumMap(p.cfg.Name, body)
	if album.ID == "" {
		alb, _, fbErr := p.searchAlbumFallback(ctx, albumProviderID)
		if fbErr == nil && alb.ID != "" {
			return alb, nil
		}
		return model.Album{}, provider.ErrNotFound
	}
	return album, nil
}

func (p *Provider) GetAlbumTracks(ctx context.Context, albumProviderID string, limit int, offset int) ([]model.Track, error) {
	if limit <= 0 {
		limit = 1000
	}
	if offset < 0 {
		offset = 0
	}

	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = joinPath(u.Path, "album")
	q := u.Query()
	q.Set("id", albumProviderID)
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		_, tracks, fbErr := p.searchAlbumFallback(ctx, albumProviderID)
		if fbErr == nil {
			return windowTracks(tracks, limit, offset), nil
		}
		return nil, err
	}
	album := normalizeAlbumMap(p.cfg.Name, body)
	if album.ID == "" {
		_, tracks, fbErr := p.searchAlbumFallback(ctx, albumProviderID)
		if fbErr == nil {
			return windowTracks(tracks, limit, offset), nil
