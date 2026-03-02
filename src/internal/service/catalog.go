package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"dhwani/internal/db"
	"dhwani/internal/model"
	"dhwani/internal/provider"
)

type CatalogService struct {
	registry               *provider.Registry
	store                  *db.Store
	logger                 *slog.Logger
	recentMu               sync.RWMutex
	recent                 map[string]recentTrack
	statsMu                sync.RWMutex
	stats                  map[string]providerStats
	providerAttemptTimeout time.Duration
	maxProviderAttempts    int
}

type recentTrack struct {
	Track  model.Track
	SeenAt time.Time
}

type providerStats struct {
	AvgLatency time.Duration
	Successes  int
	Failures   int
	LastSeen   time.Time
}

func NewCatalogService(registry *provider.Registry, store *db.Store, logger *slog.Logger) *CatalogService {
	return &CatalogService{
		registry:               registry,
		store:                  store,
		logger:                 logger,
		recent:                 map[string]recentTrack{},
		stats:                  map[string]providerStats{},
		providerAttemptTimeout: 6 * time.Second,
		maxProviderAttempts:    2,
	}
}

func (s *CatalogService) SetProviderAttemptTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	s.providerAttemptTimeout = d
}

func (s *CatalogService) SetMaxProviderAttempts(n int) {
	if n <= 0 {
		return
	}
	if n > 5 {
		n = 5
	}
	s.maxProviderAttempts = n
}

func (s *CatalogService) StartLatencyProber(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 3 * time.Hour
	}
	go func() {
		// Prime scores quickly after boot.
		s.probeProviders(ctx)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.probeProviders(ctx)
			}
		}
	}()
}

func (s *CatalogService) probeProviders(ctx context.Context) {
	for _, p := range s.registry.Enabled() {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		_, err := p.Search(callCtx, "a", 1)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			s.logger.Debug("provider latency probe failed", "provider", p.Name(), "err", err)
		}
	}
}

func (s *CatalogService) SingleProviderName() (string, bool) {
	enabled := s.registry.Enabled()
	if len(enabled) != 1 {
		return "", false
	}
	return enabled[0].Name(), true
}

func (s *CatalogService) Search(ctx context.Context, query string, limit int) (model.SearchResult, error) {
	lastErr := error(nil)
	for _, p := range s.topCandidates(s.rankProviders(s.registry.Enabled())) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		res, err := p.Search(callCtx, query, limit)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			s.logger.Warn("provider search failed", "provider", p.Name(), "err", err)
			lastErr = err
			continue
		}
		// Instances are mirrors; use first healthy result and fallback only on failure.
		dedupeSort(&res)
		return res, nil
	}
	if lastErr != nil {
		return model.SearchResult{}, lastErr
	}
	return model.SearchResult{}, nil
}

func (s *CatalogService) GetTrack(ctx context.Context, id string) (model.Track, error) {
	rawID, candidates, _, err := s.providerCandidates(id)
	if err != nil {
		return model.Track{}, err
	}
	lastErr := error(nil)
	var best model.Track
	bestScore := -1
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		track, err := p.GetTrack(callCtx, rawID)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			lastErr = err
			continue
		}
		if s.store != nil && (track.Title == "" || track.Artist == "" || track.Album == "") {
			cached, cErr := s.store.GetTrackMetadata(ctx, p.Name(), rawID)
			if cErr == nil {
				track = mergeTrackMetadata(track, cached)
			}
		}
		if strings.TrimSpace(track.Provider) == "" {
			track.Provider = p.Name()
		}
		if strings.TrimSpace(track.ProviderID) == "" {
			track.ProviderID = rawID
		}
		if strings.TrimSpace(track.ID) == "" {
			track.ID = rawID
		}
		score := trackMetadataScore(track)
		if score > bestScore {
			best = track
			bestScore = score
		}
		// Stop early if we have a high-quality track payload.
		if trackHasEssentialMetadata(track) {
			return track, nil
		}
	}
	if bestScore >= 0 {
		return best, nil
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return model.Track{}, lastErr
}

func (s *CatalogService) GetAlbum(ctx context.Context, id string) (model.Album, error) {
	rawID, candidates, _, err := s.providerCandidates(id)
	if err != nil {
		return model.Album{}, err
	}
	lastErr := error(nil)
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		album, err := p.GetAlbum(callCtx, rawID)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			lastErr = err
			continue
		}
		return album, nil
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return model.Album{}, lastErr
}

func (s *CatalogService) GetArtist(ctx context.Context, id string) (model.Artist, error) {
	rawID, candidates, _, err := s.providerCandidates(id)
	if err != nil {
		return model.Artist{}, err
	}
	lastErr := error(nil)
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		artist, err := p.GetArtist(callCtx, rawID)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			lastErr = err
			continue
		}
		return artist, nil
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return model.Artist{}, lastErr
}

func (s *CatalogService) GetArtistAlbumsLive(ctx context.Context, artistID string, limit int, offset int) ([]model.Album, error) {
	rawID, candidates, _, err := s.providerCandidates(artistID)
	if err != nil {
		return nil, err
	}
	lastErr := error(nil)
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		albums, err := p.GetArtistAlbums(callCtx, rawID, limit, offset)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			lastErr = err
			continue
		}
		return albums, nil
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return nil, lastErr
}

func (s *CatalogService) GetAlbumTracksLive(ctx context.Context, albumID string, limit int, offset int) ([]model.Track, error) {
	rawID, candidates, _, err := s.providerCandidates(albumID)
	if err != nil {
		return nil, err
	}
	lastErr := error(nil)
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		tracks, err := p.GetAlbumTracks(callCtx, rawID, limit, offset)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err != nil {
			lastErr = err
			continue
		}
		return tracks, nil
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return nil, lastErr
}

func (s *CatalogService) ResolveStream(ctx context.Context, id string) (model.StreamResolution, error) {
	rawID, candidates, _, err := s.providerCandidates(id)
	if err != nil {
		return model.StreamResolution{}, err
	}
	if len(candidates) == 0 {
		return model.StreamResolution{}, provider.ErrNotFound
	}
	started := time.Now()
	s.logger.Info("stream resolve start", "id", id, "raw_id", rawID, "providers", len(candidates))

	// For playback, race all providers and take the first successful stream resolution.
	// No hard global timeout here: each provider attempt is bounded by providerCallCtx.
	raceCtx, raceCancel := context.WithCancel(ctx)
	defer raceCancel()

	type streamResult struct {
		res model.StreamResolution
		err error
	}
	results := make(chan streamResult, len(candidates))
	var wg sync.WaitGroup
	for _, p := range candidates {
		wg.Add(1)
		go func(p provider.Provider) {
			defer wg.Done()
			callCtx, cancel := s.providerCallCtx(raceCtx)
			start := time.Now()
			res, err := p.ResolveStream(callCtx, rawID)
			cancel()
			attemptMS := time.Since(start).Milliseconds()
			if err == nil {
				s.recordProviderResult(p.Name(), time.Since(start), true)
				s.logger.Info("stream resolve attempt success", "id", id, "provider", p.Name(), "duration_ms", attemptMS)
			} else if !errors.Is(err, context.Canceled) || raceCtx.Err() == nil {
				// Avoid penalizing providers canceled after another provider already won.
				s.recordProviderResult(p.Name(), time.Since(start), false)
				s.logger.Warn("stream resolve attempt failed", "id", id, "provider", p.Name(), "duration_ms", attemptMS, "err", err)
			}
			select {
			case results <- streamResult{res: res, err: err}:
			case <-raceCtx.Done():
			}
		}(p)
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	var lastErr error
	fullMissingCount := 0
	resultCount := 0
	for r := range results {
		resultCount++
		if r.err == nil {
			raceCancel()
			s.logger.Info("stream resolve success",
				"id", id,
				"provider", r.res.Provider,
				"duration_ms", time.Since(started).Milliseconds(),
			)
			return r.res, nil
		}
		if errors.Is(r.err, provider.ErrNoFullStream) {
			fullMissingCount++
		}
		lastErr = r.err
	}
	if resultCount > 0 && fullMissingCount == resultCount {
		lastErr = provider.ErrNoFullStream
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("can't be streamed")
	}
	s.logger.Warn("stream resolve failed",
		"id", id,
		"raw_id", rawID,
		"duration_ms", time.Since(started).Milliseconds(),
		"err", lastErr,
	)
	return model.StreamResolution{}, lastErr
}

func (s *CatalogService) GetLyrics(ctx context.Context, trackID string) (model.Lyrics, error) {
	rawID, candidates, _, err := s.providerCandidates(trackID)
	if err != nil {
		return model.Lyrics{}, err
	}
	var lastErr error
	for _, p := range candidates {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		lyrics, err := p.GetLyrics(callCtx, rawID)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), err == nil)
		if err == nil {
			return lyrics, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = provider.ErrNotFound
	}
	return model.Lyrics{}, lastErr
}

func (s *CatalogService) ListArtists(ctx context.Context, limit int) ([]model.Artist, error) {
	if s.store == nil {
		return []model.Artist{}, nil
	}
	artists, err := s.store.ListCachedArtists(ctx, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.deriveArtistsFromTracks(ctx, limit)
		}
		return nil, err
	}
	if len(artists) == 0 {
		return s.deriveArtistsFromTracks(ctx, limit)
	}
	return artists, nil
}

func (s *CatalogService) ListAlbums(ctx context.Context, limit int, offset int) ([]model.Album, error) {
	if s.store == nil {
		return []model.Album{}, nil
	}
	albums, err := s.store.ListCachedAlbums(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Album{}, nil
		}
		return nil, err
	}
	return albums, nil
}

func (s *CatalogService) ListAlbumsByArtist(ctx context.Context, artistID string, limit int, offset int) ([]model.Album, error) {
	if s.store == nil {
		return []model.Album{}, nil
	}
	albums, err := s.store.ListCachedAlbumsByArtist(ctx, artistID, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Album{}, nil
		}
		return nil, err
	}
	return albums, nil
}

func (s *CatalogService) ListTracks(ctx context.Context, limit int, offset int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.ListCachedTracks(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) ListTracksByAlbum(ctx context.Context, albumID string, limit int, offset int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.ListCachedTracksByAlbum(ctx, albumID, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) ListTracksByArtist(ctx context.Context, artistID string, limit int, offset int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.ListCachedTracksByArtist(ctx, artistID, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) ListTracksByGenre(ctx context.Context, genre string, limit int, offset int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.ListCachedTracksByGenre(ctx, genre, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) GetCachedAlbum(ctx context.Context, albumID string) (model.Album, error) {
	if s.store == nil {
		return model.Album{}, sql.ErrNoRows
	}
	return s.store.GetCachedAlbum(ctx, albumID)
}

func (s *CatalogService) GetCachedAlbumAny(ctx context.Context, id string) (model.Album, error) {
	if s.store == nil {
		return model.Album{}, sql.ErrNoRows
	}
	if alb, err := s.store.GetCachedAlbum(ctx, id); err == nil {
		return alb, nil
	}
	return s.store.GetCachedAlbumAny(ctx, id)
}

func (s *CatalogService) GetAlbumMetadataAny(ctx context.Context, id string) (model.Album, error) {
	if s.store == nil {
		return model.Album{}, sql.ErrNoRows
	}
	return s.store.GetAlbumMetadataAny(ctx, id)
}

func (s *CatalogService) GetCachedArtist(ctx context.Context, artistID string) (model.Artist, error) {
	if s.store == nil {
		return model.Artist{}, sql.ErrNoRows
	}
	return s.store.GetCachedArtist(ctx, artistID)
}

func (s *CatalogService) GetCachedArtistAny(ctx context.Context, id string) (model.Artist, error) {
	if s.store == nil {
		return model.Artist{}, sql.ErrNoRows
	}
	if art, err := s.store.GetCachedArtist(ctx, id); err == nil {
		return art, nil
	}
	return s.store.GetCachedArtistAny(ctx, id)
}

func (s *CatalogService) GetCachedTrackAny(ctx context.Context, id string) (model.Track, error) {
	if s.store == nil {
		return model.Track{}, sql.ErrNoRows
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return model.Track{}, sql.ErrNoRows
	}
	if t, ok := s.recentTrack(id); ok && trackHasEssentialMetadata(t) {
		return t, nil
	}
	if t, ok := s.recentTrackByRawID(id); ok && trackHasEssentialMetadata(t) {
		return t, nil
	}
	if t, err := s.store.GetTrackMetadataByID(ctx, id); err == nil && trackHasEssentialMetadata(t) {
		return t, nil
	}
	if t, err := s.store.GetAnyTrackMetadataByProviderID(ctx, id); err == nil && trackHasEssentialMetadata(t) {
		return t, nil
	}
	return model.Track{}, sql.ErrNoRows
}

func (s *CatalogService) ResolveCoverArtURL(ctx context.Context, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if u := s.coverArtFromRecent(id); u != "" {
		return u
	}
	if t, err := s.GetCachedTrackAny(ctx, id); err == nil && strings.TrimSpace(t.CoverArtURL) != "" {
		return t.CoverArtURL
	}
	if a, err := s.GetCachedAlbumAny(ctx, id); err == nil && strings.TrimSpace(a.CoverArtURL) != "" {
		return a.CoverArtURL
	}
	if a, err := s.GetCachedArtistAny(ctx, id); err == nil && strings.TrimSpace(a.CoverArtURL) != "" {
		return a.CoverArtURL
	}
	return ""
}

func (s *CatalogService) RandomTracks(ctx context.Context, limit int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.RandomCachedTracks(ctx, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		// Fallback to latest tracks if RANDOM query path fails on an existing DB.
		fallback, fbErr := s.store.ListCachedTracks(ctx, limit, 0)
		if fbErr == nil {
			return fallback, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) ListGenres(ctx context.Context, limit int) ([]string, error) {
	if s.store == nil {
		return []string{}, nil
	}
	genres, err := s.store.ListCachedGenres(ctx, limit)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []string{}, nil
		}
		return nil, err
	}
	return genres, nil
}

func (s *CatalogService) EnsureWarmIndex(ctx context.Context, q string) {
	if s.store == nil || strings.TrimSpace(q) == "" {
		return
	}
	res, err := s.Search(ctx, q, 50)
	if err != nil {
		return
	}
	if err := s.PersistMappings(ctx, res); err != nil {
		s.logger.Warn("persist mappings failed during warm index", "query", q, "err", err)
	}
}

func (s *CatalogService) providerCandidates(id string) (rawID string, candidates []provider.Provider, preferredProvider string, err error) {
	enabled := s.registry.Enabled()
	if len(enabled) == 0 {
		return "", nil, "", fmt.Errorf("no providers enabled")
	}
	rawID = strings.TrimSpace(id)
	if rawID == "" {
		return "", nil, "", fmt.Errorf("empty id")
	}
	ranked := s.rankProviders(enabled)
	return rawID, ranked, ranked[0].Name(), nil
}

func (s *CatalogService) PersistMappings(ctx context.Context, result model.SearchResult) error {
	if s.store == nil {
		return nil
	}
	var firstErr error
	for _, a := range result.Artists {
		if err := s.store.UpsertArtistMetadata(ctx, a); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("artist mapping upsert failed", "provider", a.Provider, "provider_id", a.ProviderID, "err", err)
		}
	}
	for _, a := range result.Albums {
		if err := s.store.UpsertAlbumMetadata(ctx, a); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("album mapping upsert failed", "provider", a.Provider, "provider_id", a.ProviderID, "err", err)
		}
	}
	for _, t := range result.Tracks {
		if err := s.store.UpsertTrackMetadata(ctx, t); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			s.logger.Warn("track mapping upsert failed", "provider", t.Provider, "provider_id", t.ProviderID, "err", err)
		}
	}
	return firstErr
}

func (s *CatalogService) StarTrack(ctx context.Context, id string) error {
	if s.store == nil {
		return nil
	}
	rawID, _, preferred, err := s.providerCandidates(id)
	if err != nil {
		return err
	}
	return s.store.StarItem(ctx, "track", preferred, rawID)
}

func (s *CatalogService) StarAlbum(ctx context.Context, id string) error {
	if s.store == nil {
		return nil
	}
	rawID, _, preferred, err := s.providerCandidates(id)
	if err != nil {
		return err
	}
	return s.store.StarItem(ctx, "album", preferred, rawID)
}

func (s *CatalogService) StarArtist(ctx context.Context, id string) error {
	if s.store == nil {
		return nil
	}
	rawID, _, preferred, err := s.providerCandidates(id)
	if err != nil {
		return err
	}
	return s.store.StarItem(ctx, "artist", preferred, rawID)
}

func (s *CatalogService) ListStarredTracks(ctx context.Context, limit int, offset int) ([]model.Track, error) {
	if s.store == nil {
		return []model.Track{}, nil
	}
	tracks, err := s.store.ListStarredTracks(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Track{}, nil
		}
		return nil, err
	}
	return tracks, nil
}

func (s *CatalogService) ListStarredAlbums(ctx context.Context, limit int, offset int) ([]model.Album, error) {
	if s.store == nil {
		return []model.Album{}, nil
	}
	albums, err := s.store.ListStarredAlbums(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Album{}, nil
		}
		return nil, err
	}
	return albums, nil
}

func (s *CatalogService) ListStarredArtists(ctx context.Context, limit int, offset int) ([]model.Artist, error) {
	if s.store == nil {
		return []model.Artist{}, nil
	}
	artists, err := s.store.ListStarredArtists(ctx, limit, offset)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return []model.Artist{}, nil
		}
		return nil, err
	}
	return artists, nil
}

func (s *CatalogService) RecordPlayedTrack(ctx context.Context, id string) {
	if s.store == nil {
		return
	}
	rawID, candidates, preferred, err := s.providerCandidates(id)
	if err != nil {
		return
	}
	track := model.Track{
		ID:          rawID,
		Provider:    preferred,
		ProviderID:  rawID,
		Title:       "Track " + rawID,
		Artist:      "Unknown Artist",
		Album:       "Unknown Album",
		ArtistID:    "unknown",
		AlbumID:     "unknown",
		ContentType: "audio/flac",
	}
	if rt, ok := s.recentTrack(rawID); ok {
		track = mergeTrackMetadata(rt, track)
	}
	for _, p := range s.topCandidates(candidates) {
		callCtx, cancel := s.providerCallCtx(ctx)
		start := time.Now()
		resolved, rErr := p.GetTrack(callCtx, rawID)
		cancel()
		s.recordProviderResult(p.Name(), time.Since(start), rErr == nil)
		if rErr == nil {
			track = mergeTrackMetadata(resolved, track)
			if track.Provider == "" {
				track.Provider = p.Name()
			}
			if track.ProviderID == "" {
				track.ProviderID = rawID
			}
			if track.ID == "" {
				track.ID = rawID
			}
			break
		}
	}
	if track.Provider == "" {
		track.Provider = preferred
	}
	if track.ProviderID == "" {
		track.ProviderID = rawID
	}
	if track.ID == "" {
		track.ID = rawID
	}
	// Avoid polluting DB with synthetic placeholders when no real metadata could be resolved.
	if strings.HasPrefix(track.Title, "Track ") && track.Artist == "Unknown Artist" && track.Album == "Unknown Album" {
		return
	}

	artistProviderID := strings.TrimSpace(track.ArtistID)
	if artistProviderID == "" {
		artistProviderID = "unknown"
	}
	albumProviderID := strings.TrimSpace(track.AlbumID)
	if albumProviderID == "" {
		albumProviderID = "unknown"
	}

	if err := s.PersistMappings(ctx, model.SearchResult{
		Artists: []model.Artist{{
			ID:          track.ArtistID,
			Provider:    track.Provider,
			ProviderID:  artistProviderID,
			Name:        track.Artist,
			CoverArtURL: track.CoverArtURL,
		}},
		Albums: []model.Album{{
			ID:          track.AlbumID,
			Provider:    track.Provider,
			ProviderID:  albumProviderID,
			ArtistID:    track.ArtistID,
			Artist:      track.Artist,
			Name:        track.Album,
			CoverArtURL: track.CoverArtURL,
		}},
		Tracks: []model.Track{track},
	}); err != nil {
		s.logger.Warn("persist mappings failed during play ingest", "id", rawID, "err", err)
	}
}

func (s *CatalogService) IngestStarredTrack(ctx context.Context, id string) error {
	rawID, candidates, preferred, err := s.providerCandidates(id)
	if err != nil {
		return err
	}
	if rt, ok := s.recentTrack(rawID); ok {
		if strings.TrimSpace(rt.ID) == "" {
			rt.ID = rawID
		}
		if strings.TrimSpace(rt.Provider) == "" {
			rt.Provider = preferred
		}
		if strings.TrimSpace(rt.ProviderID) == "" {
			rt.ProviderID = rawID
		}
		if trackHasEssentialMetadata(rt) {
			return s.persistTrackSet(ctx, []model.Track{rt})
		}
	}
	// If the exact ID was not found in recent cache, try matching by raw track ID
	// across all providers to reuse richer metadata from recent search results.
	if rt, ok := s.recentTrackByRawID(rawID); ok {
		if strings.TrimSpace(rt.ID) == "" {
			rt.ID = rawID
		}
		if strings.TrimSpace(rt.Provider) == "" {
			rt.Provider = preferred
		}
		if strings.TrimSpace(rt.ProviderID) == "" {
			rt.ProviderID = rawID
		}
		if trackHasEssentialMetadata(rt) {
			return s.persistTrackSet(ctx, []model.Track{rt})
		}
	}
	if t, err := s.GetCachedTrackAny(ctx, rawID); err == nil && trackHasEssentialMetadata(t) {
		if strings.TrimSpace(t.ID) == "" {
			t.ID = rawID
		}
		if strings.TrimSpace(t.ProviderID) == "" {
			t.ProviderID = rawID
		}
		if strings.TrimSpace(t.Provider) == "" {
			t.Provider = preferred
		}
		return s.persistTrackSet(ctx, []model.Track{t})
	}
	if t, ok := s.hydrateTrackFromProviderSearch(ctx, rawID, preferred, candidates); ok {
