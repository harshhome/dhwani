package service

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"dhwani/internal/db"
	"dhwani/internal/model"
	"dhwani/internal/provider"
)

type testProvider struct {
	name          string
	searchRes     model.SearchResult
	searchErr     error
	resolveRes    model.StreamResolution
	resolveErr    error
	searchCalls   int
	resolveCalls  int
	trackResponse model.Track
}

func (p *testProvider) Name() string { return p.name }
func (p *testProvider) Type() string { return "test" }
func (p *testProvider) Search(context.Context, string, int) (model.SearchResult, error) {
	p.searchCalls++
	return p.searchRes, p.searchErr
}
func (p *testProvider) GetArtist(context.Context, string) (model.Artist, error) {
	return model.Artist{}, provider.ErrNotFound
}
func (p *testProvider) GetArtistAlbums(context.Context, string, int, int) ([]model.Album, error) {
	return nil, provider.ErrNotFound
}
func (p *testProvider) GetAlbum(context.Context, string) (model.Album, error) {
	return model.Album{}, provider.ErrNotFound
}
func (p *testProvider) GetAlbumTracks(context.Context, string, int, int) ([]model.Track, error) {
	return nil, provider.ErrNotFound
}
func (p *testProvider) GetTrack(context.Context, string) (model.Track, error) {
	if p.trackResponse.ID != "" {
		return p.trackResponse, nil
	}
	return model.Track{}, provider.ErrNotFound
}
func (p *testProvider) GetLyrics(context.Context, string) (model.Lyrics, error) {
	return model.Lyrics{}, provider.ErrNotFound
}
func (p *testProvider) ResolveStream(context.Context, string) (model.StreamResolution, error) {
	p.resolveCalls++
	return p.resolveRes, p.resolveErr
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCatalogSettersClamp(t *testing.T) {
	s := NewCatalogService(provider.NewRegistry(), nil, testLogger())

	s.SetProviderAttemptTimeout(100 * time.Second)
	if s.providerAttemptTimeout != 30*time.Second {
		t.Fatalf("expected timeout clamp to 30s, got %s", s.providerAttemptTimeout)
	}
	s.SetProviderAttemptTimeout(-1 * time.Second)
	if s.providerAttemptTimeout != 30*time.Second {
		t.Fatalf("negative timeout should be ignored")
	}

	s.SetMaxProviderAttempts(99)
	if s.maxProviderAttempts != 5 {
		t.Fatalf("expected attempt clamp to 5, got %d", s.maxProviderAttempts)
	}
	s.SetMaxProviderAttempts(0)
	if s.maxProviderAttempts != 5 {
		t.Fatalf("non-positive attempts should be ignored")
	}
}

func TestCatalogSearchFallbackAndDedupe(t *testing.T) {
	reg := provider.NewRegistry()
	p1 := &testProvider{name: "mx1", searchErr: context.DeadlineExceeded}
	p2 := &testProvider{
		name: "mx2",
		searchRes: model.SearchResult{
			Artists: []model.Artist{{ID: "ar1", Name: "A"}, {ID: "ar1", Name: "A"}},
			Albums:  []model.Album{{ID: "al1", Name: "X"}, {ID: "al1", Name: "X"}},
			Tracks:  []model.Track{{ID: "t1", Title: "T"}, {ID: "t1", Title: "T"}},
		},
	}
	if err := reg.Register(p1); err != nil {
		t.Fatalf("register p1: %v", err)
	}
	if err := reg.Register(p2); err != nil {
		t.Fatalf("register p2: %v", err)
	}

	s := NewCatalogService(reg, nil, testLogger())
	res, err := s.Search(context.Background(), "foo", 10)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(res.Artists) != 1 || len(res.Albums) != 1 || len(res.Tracks) != 1 {
		t.Fatalf("expected deduped result, got: %#v", res)
	}
	if p1.searchCalls == 0 || p2.searchCalls == 0 {
		t.Fatalf("expected fallback to second provider, calls p1=%d p2=%d", p1.searchCalls, p2.searchCalls)
	}
}

func TestCatalogResolveStreamAllNoFull(t *testing.T) {
	reg := provider.NewRegistry()
	p1 := &testProvider{name: "mx1", resolveErr: provider.ErrNoFullStream}
	p2 := &testProvider{name: "mx2", resolveErr: provider.ErrNoFullStream}
	_ = reg.Register(p1)
	_ = reg.Register(p2)

	s := NewCatalogService(reg, nil, testLogger())
	_, err := s.ResolveStream(context.Background(), "123")
	if !errors.Is(err, provider.ErrNoFullStream) {
		t.Fatalf("expected ErrNoFullStream, got %v", err)
	}
}

func TestRememberTracksAndRawLookup(t *testing.T) {
	s := NewCatalogService(provider.NewRegistry(), nil, testLogger())
	s.RememberTracks([]model.Track{{ID: "a", ProviderID: "raw-a", Title: "Song"}})

	got, ok := s.recentTrackByRawID("raw-a")
	if !ok || got.ID != "a" {
		t.Fatalf("recentTrackByRawID failed, ok=%v track=%#v", ok, got)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(provider.ErrNotFound) {
		t.Fatalf("provider.ErrNotFound should be treated as not-found")
	}
	if IsNotFound(context.Canceled) {
		t.Fatalf("context.Canceled should not be treated as not-found")
	}
}

func TestSingleProviderNameAndTopCandidates(t *testing.T) {
	reg := provider.NewRegistry()
	p1 := &testProvider{name: "mx1"}
	p2 := &testProvider{name: "mx2"}
	if err := reg.Register(p1); err != nil {
		t.Fatalf("register p1: %v", err)
	}
	s := NewCatalogService(reg, nil, testLogger())
	name, ok := s.SingleProviderName()
	if !ok || name != "mx1" {
		t.Fatalf("unexpected single provider result: ok=%v name=%q", ok, name)
	}

	if err := reg.Register(p2); err != nil {
		t.Fatalf("register p2: %v", err)
	}
	if _, ok := s.SingleProviderName(); ok {
		t.Fatalf("expected single provider false with two providers")
	}

	s.SetMaxProviderAttempts(1)
	top := s.topCandidates(reg.Enabled())
	if len(top) != 1 {
		t.Fatalf("expected topCandidates limit 1, got %d", len(top))
	}
}

func TestTrackMergeAndScoreHelpers(t *testing.T) {
	base := model.Track{ID: "t1", Title: "", Artist: "", Album: "", ContentType: ""}
	cached := model.Track{
		ID:          "t1",
		Title:       "Song",
		Artist:      "Artist",
		Album:       "Album",
		ArtistID:    "ar1",
		AlbumID:     "al1",
		ContentType: "audio/flac",
		CoverArtURL: "https://img",
	}
	merged := mergeTrackMetadata(base, cached)
	if !trackHasEssentialMetadata(merged) {
		t.Fatalf("expected essential metadata after merge: %#v", merged)
	}
	if score := trackMetadataScore(merged); score < 6 {
		t.Fatalf("unexpected track metadata score: %d", score)
	}
}

func TestProviderRankScoreAndRankProviders(t *testing.T) {
	fast := providerRankScore(providerStats{AvgLatency: 50 * time.Millisecond, Successes: 5, Failures: 0})
	slow := providerRankScore(providerStats{AvgLatency: 500 * time.Millisecond, Successes: 5, Failures: 0})
	if !(fast < slow) {
		t.Fatalf("expected fast score < slow score, fast=%f slow=%f", fast, slow)
	}

	reg := provider.NewRegistry()
	p1 := &testProvider{name: "mx1"}
	p2 := &testProvider{name: "mx2"}
	_ = reg.Register(p1)
	_ = reg.Register(p2)
	s := NewCatalogService(reg, nil, testLogger())
	s.stats["mx1"] = providerStats{AvgLatency: 2 * time.Second, Successes: 1, Failures: 3}
	s.stats["mx2"] = providerStats{AvgLatency: 100 * time.Millisecond, Successes: 3, Failures: 0}

	ranked := s.rankProviders(reg.Enabled())
	if len(ranked) != 2 || ranked[0].Name() != "mx2" {
		t.Fatalf("unexpected ranked providers: %#v", ranked)
	}
}

func TestStartLatencyProberAndNilStoreBranches(t *testing.T) {
	reg := provider.NewRegistry()
	p := &testProvider{name: "mx1", searchRes: model.SearchResult{}}
	_ = reg.Register(p)
	s := NewCatalogService(reg, nil, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	s.StartLatencyProber(ctx, 5*time.Millisecond)
	time.Sleep(15 * time.Millisecond)
	cancel()
	if p.searchCalls == 0 {
		t.Fatalf("expected prober to call Search at least once")
	}

	// nil store wrappers
	if artists, err := s.ListArtists(context.Background(), 10); err != nil || len(artists) != 0 {
		t.Fatalf("ListArtists nil-store expected empty,nil got %v %#v", err, artists)
	}
	if _, err := s.GetCachedAlbum(context.Background(), "x"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetCachedAlbum nil-store should return sql.ErrNoRows, got %v", err)
	}
	if tracks, err := s.RandomTracks(context.Background(), 1); err != nil || len(tracks) != 0 {
		t.Fatalf("RandomTracks nil-store expected empty,nil got %v %#v", err, tracks)
	}
}

func TestStoreNilDBErrNoRowsBranches(t *testing.T) {
	s := NewCatalogService(provider.NewRegistry(), &db.Store{}, testLogger())
	ctx := context.Background()

	if artists, err := s.ListArtists(ctx, 10); err != nil || len(artists) != 0 {
		t.Fatalf("ListArtists nil-db expected empty,nil got err=%v artists=%#v", err, artists)
	}
	if albums, err := s.ListAlbums(ctx, 10, 0); err != nil || len(albums) != 0 {
		t.Fatalf("ListAlbums nil-db expected empty,nil got err=%v albums=%#v", err, albums)
	}
	if tracks, err := s.ListTracks(ctx, 10, 0); err != nil || len(tracks) != 0 {
		t.Fatalf("ListTracks nil-db expected empty,nil got err=%v tracks=%#v", err, tracks)
	}
	if genres, err := s.ListGenres(ctx, 10); err != nil || len(genres) != 0 {
		t.Fatalf("ListGenres nil-db expected empty,nil got err=%v genres=%#v", err, genres)
	}
	if tracks, err := s.RandomTracks(ctx, 1); err != nil || len(tracks) != 0 {
		t.Fatalf("RandomTracks nil-db expected empty,nil got err=%v tracks=%#v", err, tracks)
	}
}
