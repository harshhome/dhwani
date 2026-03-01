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
		}
		return nil, provider.ErrNotFound
	}

	items := extractList(body, "items", "tracks", "songs", "song")
	if len(items) == 0 {
		if inner := getMap(body, "album"); inner != nil {
			items = extractList(inner, "items", "tracks", "songs", "song")
		}
	}

	out := make([]model.Track, 0, len(items))
	for _, it := range items {
		raw := it
		if nested := getMap(it, "item"); nested != nil {
			raw = nested
		}
		typ := strings.ToLower(getString(it, "type"))
		if typ != "" && typ != "track" {
			continue
		}
		t := normalizeTrackMap(p.cfg.Name, raw)
		if t.ID == "" {
			continue
		}
		if t.AlbumID == "" {
			t.AlbumID = album.ID
		}
		if t.Album == "" {
			t.Album = album.Name
		}
		if t.ArtistID == "" {
			t.ArtistID = album.ArtistID
		}
		if t.Artist == "" {
			t.Artist = album.Artist
		}
		if t.CoverArtURL == "" {
			t.CoverArtURL = album.CoverArtURL
		}
		out = append(out, t)
	}

	return windowTracks(out, limit, offset), nil
}

func (p *Provider) GetTrack(ctx context.Context, trackProviderID string) (model.Track, error) {
	var out model.Track
	if infoPayload, err := p.fetchTrackInfoPayload(ctx, trackProviderID); err == nil {
		src := infoPayload
		for _, key := range []string{"track", "item", "song"} {
			if nested := getMap(infoPayload, key); nested != nil {
				src = nested
				break
			}
		}
		out = normalizeTrackMap(p.cfg.Name, src)
	}
	if payload, err := p.fetchTrackPayload(ctx, trackProviderID); err == nil {
		src := payload
		for _, key := range []string{"track", "item", "song"} {
			if nested := getMap(payload, key); nested != nil {
				src = nested
				break
			}
		}
		streamTrack := normalizeTrackMap(p.cfg.Name, src)
		if streamTrack.ContentType == "" {
			streamTrack.ContentType = firstNonEmpty(getString(payload, "manifestMimeType"), getString(payload, "mimeType"), "audio/flac")
		}
		out = mergeTrackFields(out, streamTrack)
	}
	if out.ID == "" {
		out = model.Track{
			ID:          trackProviderID,
			Provider:    p.cfg.Name,
			ProviderID:  trackProviderID,
			ContentType: "audio/flac",
		}
	}

	metaTrack, err := p.findTrackBySearch(ctx, trackProviderID)
	if err == nil {
		if metaTrack.ID != "" {
			out = mergeTrackFields(out, metaTrack)
		}
	}

	if out.Provider == "" {
		out.Provider = p.cfg.Name
	}
	if out.ProviderID == "" {
		out.ProviderID = trackProviderID
	}
	if out.ID == "" {
		out.ID = trackProviderID
	}
	return out, nil
}

func (p *Provider) GetLyrics(ctx context.Context, trackProviderID string) (model.Lyrics, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.Lyrics{}, err
	}
	u.Path = joinPath(u.Path, "lyrics")
	q := u.Query()
	q.Set("id", trackProviderID)
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()

	body, err := p.getJSON(ctx, u.String())
	if err != nil {
		return model.Lyrics{}, err
	}
	payload := body
	if nested := getMap(body, "lyrics"); nested != nil {
		payload = nested
	}

	subtitles := firstNonEmpty(getString(payload, "subtitles"), getString(payload, "subtitle"), getString(payload, "lrc"))
	lines := parseLRCSubtitles(subtitles)
	text := firstNonEmpty(getString(payload, "lyrics"), getString(payload, "text"), getString(payload, "lyric"), getString(payload, "body"))
	if text == "" {
		if ls := extractLyricsLines(payload); len(ls) > 0 {
			text = strings.Join(ls, "\n")
		} else if len(lines) > 0 {
			plain := make([]string, 0, len(lines))
			for _, ln := range lines {
				plain = append(plain, ln.Value)
			}
			text = strings.Join(plain, "\n")
		}
	}
	if strings.TrimSpace(text) == "" {
		return model.Lyrics{}, provider.ErrNotFound
	}
	out := model.Lyrics{
		Artist: firstNonEmpty(getString(payload, "artist"), getString(payload, "artistName")),
		Title:  firstNonEmpty(getString(payload, "title"), getString(payload, "track"), getString(payload, "song")),
		Text:   text,
		Lines:  lines,
	}
	return out, nil
}

func (p *Provider) ResolveStream(ctx context.Context, trackProviderID string) (model.StreamResolution, error) {
	payload, err := p.fetchTrackPayload(ctx, trackProviderID)
	if err != nil {
		return model.StreamResolution{}, err
	}
	assetPresentation := strings.ToUpper(strings.TrimSpace(getString(payload, "assetPresentation")))
	if assetPresentation != "" && assetPresentation != "FULL" {
		return model.StreamResolution{}, fmt.Errorf("%w: assetPresentation=%s", provider.ErrNoFullStream, assetPresentation)
	}
	manifestMIME := firstNonEmpty(getString(payload, "manifestMimeType"), getString(payload, "mimeType"))
	manifestB64 := getString(payload, "manifest", "manifestBase64")
	if manifestB64 == "" {
		return model.StreamResolution{}, fmt.Errorf("manifest missing for track %s", trackProviderID)
	}
	base := model.StreamResolution{
		Provider:          p.cfg.Name,
		TrackID:           trackProviderID,
		TrackProviderID:   trackProviderID,
		ManifestMIME:      manifestMIME,
		ManifestBase64:    manifestB64,
		ManifestHash:      getString(payload, "manifestHash"),
		AssetPresentation: assetPresentation,
		ResolvedAt:        time.Now().UTC(),
	}
	if strings.EqualFold(manifestMIME, "application/dash+xml") {
		return base, nil
	}
	manifest, err := DecodeManifest(manifestB64)
	if err != nil {
		return model.StreamResolution{}, fmt.Errorf("decode manifest: %w", err)
	}
	if len(manifest.URLs) == 0 || manifest.URLs[0] == "" {
		return model.StreamResolution{}, fmt.Errorf("no media urls in manifest")
	}
	base.ManifestMIME = firstNonEmpty(manifest.MIMEType, base.ManifestMIME)
	base.MediaURL = manifest.URLs[0]
	return base, nil
}

func (p *Provider) fetchTrackPayload(ctx context.Context, trackProviderID string) (map[string]any, error) {
	preferred := provider.PreferredQuality(ctx)
	qualities := []string{}
	if provider.StrictQuality(ctx) {
		if preferred != "" {
			qualities = append(qualities, preferred)
		}
	}
	if len(qualities) == 0 && preferred == "" {
		preferred = p.cfg.StreamQuality
	}
	if len(qualities) == 0 {
		qualities = candidateQualities(preferred)
	}
	var lastErr error
	for _, quality := range qualities {
		u, err := url.Parse(p.cfg.BaseURL)
		if err != nil {
			return nil, err
		}
		u.Path = joinPath(u.Path, "track")
		q := u.Query()
		q.Set("id", trackProviderID)
		if p.cfg.Source != "" {
			q.Set("source", p.cfg.Source)
		}
		if quality != "" {
			q.Set("quality", quality)
		}
		u.RawQuery = q.Encode()

		body, err := p.getJSON(ctx, u.String())
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !isRetryableProviderStatus(err) {
			return nil, err
		}
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return nil, lastErr
}

func (p *Provider) fetchTrackInfoPayload(ctx context.Context, trackProviderID string) (map[string]any, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = joinPath(u.Path, "info")
	q := u.Query()
