package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"dhwani/internal/model"
)

func seedSmokeTrack(t *testing.T, store interface {
	UpsertTrackMetadata(context.Context, model.Track) error
}) {
	t.Helper()
	if err := store.UpsertTrackMetadata(context.Background(), model.Track{
		ID:          "t-smoke",
		Provider:    "mx1",
		ProviderID:  "t-smoke",
		Title:       "Smoke Song",
		Artist:      "Smoke Artist",
		Album:       "Smoke Album",
		Genre:       "Rock",
		ArtistID:    "ar-smoke",
		AlbumID:     "al-smoke",
		ContentType: "audio/flac",
		CoverArtURL: "https://img/smoke.jpg",
	}); err != nil {
		t.Fatalf("seed track: %v", err)
	}
}

func TestHandlerSmokeCoverage(t *testing.T) {
	fp := &fakeProvider{
		track:  model.Track{ID: "t-smoke", Provider: "mx1", ProviderID: "t-smoke", Title: "Smoke Song", Artist: "Smoke Artist", Album: "Smoke Album", ArtistID: "ar-smoke", AlbumID: "al-smoke", ContentType: "audio/flac"},
		artist: model.Artist{ID: "ar-smoke", Name: "Smoke Artist", CoverArtURL: "https://img/artist.jpg"},
		album:  model.Album{ID: "al-smoke", Name: "Smoke Album", Artist: "Smoke Artist", ArtistID: "ar-smoke", CoverArtURL: "https://img/album.jpg"},
	}
	srv, store := newTestServer(t, fp, false)
	defer store.Close()
	seedSmokeTrack(t, store)

	paths := []string{
		"/rest/getLicense.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getMusicFolders.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getIndexes.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getArtists.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getMusicDirectory.view?u=u&p=p&v=1.16.1&c=test&f=json&id=1",
		"/rest/getMusicDirectory.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-ar-smoke",
		"/rest/getMusicDirectory.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-smoke",
		"/rest/getAlbumList.view?u=u&p=p&v=1.16.1&c=test&f=json&size=10",
		"/rest/getAlbumList2.view?u=u&p=p&v=1.16.1&c=test&f=json&size=10",
		"/rest/getArtistInfo.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-smoke",
		"/rest/getArtistInfo2.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-smoke",
		"/rest/getAlbumInfo.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-smoke",
		"/rest/getAlbumInfo2.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-smoke",
		"/rest/getPlaylists.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getPlaylist.view?u=u&p=p&v=1.16.1&c=test&f=json&id=p1",
		"/rest/getStarred.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getStarred2.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getNowPlaying.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getRandomSongs.view?u=u&p=p&v=1.16.1&c=test&f=json&size=1",
		"/rest/getSongsByGenre.view?u=u&p=p&v=1.16.1&c=test&f=json&genre=Rock&size=10",
		"/rest/getGenres.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getScanStatus.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/getUser.view?u=u&p=p&v=1.16.1&c=test&f=json",
		"/rest/search3.view?u=u&p=p&v=1.16.1&c=test&f=json&query=&artistCount=5&albumCount=5&songCount=5",
		"/rest/getSong.view?u=u&p=p&v=1.16.1&c=test&f=json&id=t-smoke",
		"/rest/getArtist.view?u=u&p=p&v=1.16.1&c=test&f=json&id=ar-smoke",
		"/rest/getAlbum.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-smoke",
		"/rest/getCoverArt.view?u=u&p=p&v=1.16.1&c=test&f=json&id=al-al-smoke",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code >= 500 {
			t.Fatalf("path %s returned status %d body=%s", path, rr.Code, rr.Body.String())
		}
	}
}
