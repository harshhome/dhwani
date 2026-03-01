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
	q.Set("id", trackProviderID)
	if p.cfg.Source != "" {
		q.Set("source", p.cfg.Source)
	}
	u.RawQuery = q.Encode()
	return p.getJSON(ctx, u.String())
}

func (p *Provider) findTrackBySearch(ctx context.Context, trackProviderID string) (model.Track, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.Track{}, err
	}
	u.Path = joinPath(u.Path, "search")
	q := u.Query()
	q.Set("s", trackProviderID)
	q.Set("limit", "50")
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
		return model.Track{}, err
	}
	for _, t := range extractList(body, "items", "tracks", "songs", "song", "results") {
		tr := normalizeTrackMap(p.cfg.Name, t)
		if tr.ProviderID == trackProviderID {
			return tr, nil
		}
	}
	return model.Track{}, provider.ErrNotFound
}

func (p *Provider) searchAlbumFallback(ctx context.Context, albumProviderID string) (model.Album, []model.Track, error) {
	u, err := url.Parse(p.cfg.BaseURL)
	if err != nil {
		return model.Album{}, nil, err
	}
	u.Path = joinPath(u.Path, "search")
	q := u.Query()
	q.Set("s", albumProviderID)
	q.Set("limit", "500")
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
		return model.Album{}, nil, err
	}
	targetAlbumID := albumProviderID
	tracks := make([]model.Track, 0)
	for _, t := range extractList(body, "tracks", "songs", "song", "results", "items") {
		tr := normalizeTrackMap(p.cfg.Name, t)
		if tr.ID == "" || tr.AlbumID != targetAlbumID {
			continue
		}
		tracks = append(tracks, tr)
	}
	if len(tracks) == 0 {
		return model.Album{}, nil, provider.ErrNotFound
	}
	sort.SliceStable(tracks, func(i, j int) bool {
		if tracks[i].DiscNumber != tracks[j].DiscNumber {
			return tracks[i].DiscNumber < tracks[j].DiscNumber
		}
		if tracks[i].TrackNumber != tracks[j].TrackNumber {
			return tracks[i].TrackNumber < tracks[j].TrackNumber
		}
		return strings.ToLower(tracks[i].Title) < strings.ToLower(tracks[j].Title)
	})
	dur := 0
	for _, t := range tracks {
		dur += t.DurationSec
	}
	alb := model.Album{
		ID:          targetAlbumID,
		Provider:    p.cfg.Name,
		ProviderID:  albumProviderID,
		Name:        tracks[0].Album,
		Artist:      tracks[0].Artist,
		ArtistID:    tracks[0].ArtistID,
		SongCount:   len(tracks),
		DurationSec: dur,
		CoverArtURL: tracks[0].CoverArtURL,
	}
	return alb, tracks, nil
}

func (p *Provider) getJSON(ctx context.Context, target string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if p.cfg.ClientHeader != "" {
		req.Header.Set("x-client", p.cfg.ClientHeader)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, provider.ErrNotFound
	}
	if resp.StatusCode >= 400 {
		return nil, providerStatusError{statusCode: resp.StatusCode}
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return unwrap(payload), nil
}

type TrackManifest struct {
	MIMEType       string   `json:"mimeType"`
	Codecs         string   `json:"codecs"`
	EncryptionType string   `json:"encryptionType"`
	URLs           []string `json:"urls"`
}

func DecodeManifest(manifestB64 string) (TrackManifest, error) {
	b, err := base64.StdEncoding.DecodeString(manifestB64)
	if err != nil {
		return TrackManifest{}, fmt.Errorf("base64 decode: %w", err)
	}
	var m TrackManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return TrackManifest{}, fmt.Errorf("manifest json decode: %w", err)
	}
	return m, nil
}

func normalizeSearch(providerName string, payload map[string]any) model.SearchResult {
	result := model.SearchResult{}
	artists := extractList(payload, "artists", "artist", "artistResults")
	for _, a := range artists {
		result.Artists = append(result.Artists, normalizeArtistMap(providerName, a))
	}
	albums := extractList(payload, "albums", "album", "albumResults")
	for _, a := range albums {
		result.Albums = append(result.Albums, normalizeAlbumMap(providerName, a))
	}
	tracks := extractList(payload, "tracks", "songs", "song", "results", "items")
	for _, t := range tracks {
		if tr := normalizeTrackMap(providerName, t); tr.ID != "" {
			result.Tracks = append(result.Tracks, tr)
			if tr.ArtistID != "" {
				result.Artists = append(result.Artists, model.Artist{
					ID:         tr.ArtistID,
					Provider:   providerName,
					ProviderID: tr.ArtistID,
					Name:       tr.Artist,
				})
			}
			if tr.AlbumID != "" {
				result.Albums = append(result.Albums, model.Album{
					ID:         tr.AlbumID,
					Provider:   providerName,
					ProviderID: tr.AlbumID,
					ArtistID:   tr.ArtistID,
					Artist:     tr.Artist,
					Name:       tr.Album,
				})
			}
		}
	}
	return result
}

func normalizeArtistMap(providerName string, m map[string]any) model.Artist {
	src := m
	if nested := getMap(m, "artist"); nested != nil {
		src = nested
	}
	coverMap := getMap(m, "cover")

	providerID := firstNonEmpty(
		getString(src, "id"),
		getString(src, "artistId"),
		getString(src, "uuid"),
	)
	if providerID == "" {
		return model.Artist{}
	}
	return model.Artist{
		ID:         providerID,
		Provider:   providerName,
		ProviderID: providerID,
		Name:       firstNonEmpty(getString(src, "name"), getString(src, "artist")),
		CoverArtURL: normalizeImageRef(firstNonEmpty(
			getString(src, "cover"),
			getString(src, "image"),
			getString(src, "coverArt"),
			getString(src, "picture"),
			getString(coverMap, "750"),
			getString(coverMap, "640"),
			getString(coverMap, "320"),
		)),
	}
}

func normalizeAlbumMap(providerName string, m map[string]any) model.Album {
	providerID := firstNonEmpty(
		getString(m, "id"),
		getString(m, "albumId"),
	)
	if providerID == "" {
		return model.Album{}
	}
	artistObj := getMap(m, "artist")
	artistProviderID := firstNonEmpty(getString(m, "artistId"), getString(m, "artist_id"), getString(artistObj, "id"))
	artistName := firstNonEmpty(getString(m, "artist"), getString(m, "artistName"), getString(artistObj, "name"))
	if artistProviderID == "" || artistName == "" {
		mainID, mainName := pickMainAlbumArtist(m)
		if artistProviderID == "" {
			artistProviderID = mainID
		}
		if artistName == "" {
			artistName = mainName
		}
	}
	artistID := ""
	if artistProviderID != "" {
		artistID = artistProviderID
	}
	yearRaw := firstNonEmpty(
		getString(m, "year"),
		getString(m, "releaseYear"),
		yearFromDateString(getString(m, "releaseDate")),
		yearFromDateString(getString(m, "originalReleaseDate")),
	)
	if yearRaw == "" {
		streamStartDate := strings.TrimSpace(getString(m, "streamStartDate"))
		if len(streamStartDate) >= 4 {
			yearRaw = streamStartDate[:4]
		}
	}
	year, _ := strconv.Atoi(yearRaw)
	return model.Album{
		ID:          providerID,
		Provider:    providerName,
		ProviderID:  providerID,
		ArtistID:    artistID,
		Artist:      artistName,
		Name:        firstNonEmpty(getString(m, "title"), getString(m, "name"), getString(m, "album")),
		Year:        year,
		CoverArtURL: normalizeImageRef(firstNonEmpty(getString(m, "cover"), getString(m, "image"), getString(m, "coverArt"))),
	}
}

func pickMainAlbumArtist(m map[string]any) (id string, name string) {
	artists := extractList(m, "artists", "artist")
	if len(artists) == 0 {
		return "", ""
	}
	for _, a := range artists {
		typ := strings.ToUpper(strings.TrimSpace(getString(a, "type")))
		if typ == "MAIN" {
			return firstNonEmpty(getString(a, "id"), getString(a, "artistId"), getString(a, "artist_id")),
				firstNonEmpty(getString(a, "name"), getString(a, "artist"), getString(a, "artistName"))
		}
	}
	a := artists[0]
	return firstNonEmpty(getString(a, "id"), getString(a, "artistId"), getString(a, "artist_id")),
		firstNonEmpty(getString(a, "name"), getString(a, "artist"), getString(a, "artistName"))
}

func yearFromDateString(v string) string {
	v = strings.TrimSpace(v)
	if len(v) < 4 {
		return ""
	}
	y := v[:4]
	for _, r := range y {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return y
}

func normalizeTrackMap(providerName string, m map[string]any) model.Track {
	albumObj := getMap(m, "album")
	artists := extractTrackArtists(m)
	var artistObj map[string]any
	if len(artists) > 0 {
		artistObj = map[string]any{
			"id":   artists[0].ID,
			"name": artists[0].Name,
		}
	}

	providerID := firstNonEmpty(getString(m, "trackId"), getString(m, "id"), getString(m, "songId"))
	if providerID == "" {
		return model.Track{}
	}
	albumProviderID := firstNonEmpty(getString(m, "albumId"), getString(m, "album_id"), getString(albumObj, "id"))
	artistProviderID := firstNonEmpty(getString(m, "artistId"), getString(m, "artist_id"), getString(artistObj, "id"))

	albumID := ""
	if albumProviderID != "" {
		albumID = albumProviderID
	}
	artistID := ""
	if artistProviderID != "" {
		artistID = artistProviderID
	}

	dur, _ := strconv.Atoi(firstNonEmpty(getString(m, "duration"), getString(m, "durationSec"), getString(m, "durationSeconds")))
	bitRate, _ := strconv.Atoi(getString(m, "bitRate"))
	trackNo, _ := strconv.Atoi(firstNonEmpty(getString(m, "trackNumber"), getString(m, "track")))
	discNo, _ := strconv.Atoi(firstNonEmpty(getString(m, "volumeNumber"), getString(m, "discNumber")))
	displayArtist := formatDisplayArtistNames(artists)

	return model.Track{
		ID:            providerID,
		Provider:      providerName,
		ProviderID:    providerID,
		ProviderRawID: providerID,
		AlbumID:       albumID,
		ArtistID:      artistID,
		Title:         firstNonEmpty(getString(m, "title"), getString(m, "name")),
		Artist:        firstNonEmpty(getString(m, "artist"), getString(m, "artistName"), getString(artistObj, "name")),
		DisplayArtist: displayArtist,
		Artists:       artists,
		Album:         firstNonEmpty(getString(m, "album"), getString(m, "albumTitle"), getString(albumObj, "title"), getString(albumObj, "name")),
		Genre:         firstNonEmpty(getString(m, "genre"), getString(m, "genreName")),
		DurationSec:   dur,
		TrackNumber:   trackNo,
		DiscNumber:    discNo,
		BitRate:       bitRate,
		ContentType:   firstNonEmpty(getString(m, "manifestMimeType"), getString(m, "mimeType"), "audio/flac"),
		CoverArtURL: normalizeImageRef(firstNonEmpty(
			getString(m, "cover"),
			getString(m, "image"),
			getString(m, "coverArt"),
			getString(albumObj, "cover"),
			getString(artistObj, "picture"),
		)),
	}
}

func extractList(m map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch arr := v.(type) {
		case []any:
			out := make([]map[string]any, 0, len(arr))
			for _, item := range arr {
				if mm, ok := item.(map[string]any); ok {
					out = append(out, mm)
				}
			}
			return out
		case map[string]any:
			return []map[string]any{arr}
		}
	}
	return nil
}

func unwrap(payload map[string]any) map[string]any {
	for _, key := range []string{"data", "result", "results", "response"} {
		if v, ok := payload[key].(map[string]any); ok {
			return v
		}
	}
	return payload
}

func getString(m map[string]any, keys ...string) string {
	if m == nil {
		return ""
	}
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if t != "" {
				return t
			}
		case json.Number:
			return t.String()
		case float64:
			return strconv.FormatInt(int64(t), 10)
		case int:
			return strconv.Itoa(t)
		case int64:
			return strconv.FormatInt(t, 10)
		}
	}
	return ""
}

func getMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func extractLyricsLines(m map[string]any) []string {
	keys := []string{"lines", "lyrics", "lyric"}
	for _, key := range keys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		out := make([]string, 0, len(arr))
		for _, item := range arr {
			switch t := item.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					out = append(out, t)
				}
			case map[string]any:
				line := firstNonEmpty(getString(t, "text"), getString(t, "line"), getString(t, "lyrics"))
				if strings.TrimSpace(line) != "" {
					out = append(out, line)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return nil
}

func parseLRCSubtitles(raw string) []model.LyricLine {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	out := make([]model.LyricLine, 0, len(lines))
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if !strings.HasPrefix(ln, "[") {
			continue
		}
		end := strings.IndexByte(ln, ']')
		if end <= 1 {
			continue
		}
		ts := ln[1:end]
		text := strings.TrimSpace(ln[end+1:])
