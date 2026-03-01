package provider

import (
	"context"
	"testing"

	"dhwani/internal/model"
)

type dummyProvider struct {
	name string
}

func (d dummyProvider) Name() string { return d.name }
func (d dummyProvider) Type() string { return "dummy" }
func (d dummyProvider) Search(context.Context, string, int) (model.SearchResult, error) {
	return model.SearchResult{}, nil
}
func (d dummyProvider) GetArtist(context.Context, string) (model.Artist, error) {
	return model.Artist{}, nil
}
func (d dummyProvider) GetArtistAlbums(context.Context, string, int, int) ([]model.Album, error) {
	return nil, nil
}
func (d dummyProvider) GetAlbum(context.Context, string) (model.Album, error) {
	return model.Album{}, nil
}
func (d dummyProvider) GetAlbumTracks(context.Context, string, int, int) ([]model.Track, error) {
	return nil, nil
}
func (d dummyProvider) GetTrack(context.Context, string) (model.Track, error) {
	return model.Track{}, nil
}
func (d dummyProvider) GetLyrics(context.Context, string) (model.Lyrics, error) {
	return model.Lyrics{}, nil
}
func (d dummyProvider) ResolveStream(context.Context, string) (model.StreamResolution, error) {
	return model.StreamResolution{}, nil
}

func TestRegistryRegisterGetEnabled(t *testing.T) {
	r := NewRegistry()
	p1 := dummyProvider{name: "mx1"}
	p2 := dummyProvider{name: "mx2"}

	if err := r.Register(p1); err != nil {
		t.Fatalf("register p1: %v", err)
	}
	if err := r.Register(p2); err != nil {
		t.Fatalf("register p2: %v", err)
	}

	got, ok := r.Get("mx1")
	if !ok || got.Name() != "mx1" {
		t.Fatalf("Get(mx1) failed, ok=%v got=%v", ok, got)
	}

	enabled := r.Enabled()
	if len(enabled) != 2 || enabled[0].Name() != "mx1" || enabled[1].Name() != "mx2" {
		t.Fatalf("unexpected enabled order: %#v", enabled)
	}
}

func TestRegistryRegisterErrors(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Fatalf("expected nil provider error")
	}

	if err := r.Register(dummyProvider{name: ""}); err == nil {
		t.Fatalf("expected empty provider name error")
	}

	if err := r.Register(dummyProvider{name: "mx1"}); err != nil {
		t.Fatalf("register first provider: %v", err)
	}
	if err := r.Register(dummyProvider{name: "mx1"}); err == nil {
		t.Fatalf("expected duplicate provider error")
	}
}
