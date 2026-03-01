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
