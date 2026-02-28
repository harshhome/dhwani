package provider

import (
	"context"
	"errors"
	"fmt"

	"dhwani/internal/model"
)

var ErrNotFound = errors.New("not found")
var ErrNoFullStream = errors.New("no FULL stream available")

type Provider interface {
	Name() string
	Type() string
	Search(ctx context.Context, query string, limit int) (model.SearchResult, error)
	GetArtist(ctx context.Context, artistProviderID string) (model.Artist, error)
	GetArtistAlbums(ctx context.Context, artistProviderID string, limit int, offset int) ([]model.Album, error)
	GetAlbum(ctx context.Context, albumProviderID string) (model.Album, error)
	GetAlbumTracks(ctx context.Context, albumProviderID string, limit int, offset int) ([]model.Track, error)
	GetTrack(ctx context.Context, trackProviderID string) (model.Track, error)
	GetLyrics(ctx context.Context, trackProviderID string) (model.Lyrics, error)
	ResolveStream(ctx context.Context, trackProviderID string) (model.StreamResolution, error)
}

type Registry struct {
	providers map[string]Provider
	order     []string
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("provider is nil")
	}
	name := p.Name()
	if name == "" {
		return fmt.Errorf("provider name is empty")
	}
	if _, ok := r.providers[name]; ok {
		return fmt.Errorf("provider %q already registered", name)
	}
	r.providers[name] = p
	r.order = append(r.order, name)
	return nil
}

func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

func (r *Registry) Enabled() []Provider {
	out := make([]Provider, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.providers[name])
	}
	return out
}
