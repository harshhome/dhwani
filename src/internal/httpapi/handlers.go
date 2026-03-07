package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dhwani/internal/model"
	"dhwani/internal/provider"
	"dhwani/internal/service"
	"dhwani/internal/subsonic"
)

func (s *Server) ping(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(nil))
}

func (s *Server) getLicense(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{License: &subsonic.License{Valid: true}}))
}

func (s *Server) getMusicFolders(w http.ResponseWriter, r *http.Request) {
	payload := &subsonic.PayloadUnion{MusicFolders: &subsonic.MusicFolders{Folders: []subsonic.MusicFolder{{ID: 1, Name: "Dhwani"}}}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getIndexes(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	artists, err := s.catalog.ListArtists(ctx, 5000)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list indexes"))
		return
	}
	indexMap := map[string][]subsonic.Artist{}
	for _, a := range artists {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		bucket := strings.ToUpper(string([]rune(name)[0]))
		if bucket < "A" || bucket > "Z" {
			bucket = "#"
		}
		indexMap[bucket] = append(indexMap[bucket], subsonic.Artist{
			ID:             s.artistCoverArtID(a.ID),
			Name:           a.Name,
			CoverArt:       s.artistCoverArtID(a.ID),
			ArtistImageURL: a.CoverArtURL,
		})
	}
	keys := make([]string, 0, len(indexMap))
	for k := range indexMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	indexes := make([]subsonic.Index, 0, len(keys))
	for _, k := range keys {
		indexes = append(indexes, subsonic.Index{Name: k, Artists: indexMap[k]})
	}
	payload := &subsonic.PayloadUnion{Indexes: &subsonic.Indexes{IgnoredArticles: "The El La Los Las Le Les", LastModified: time.Now().UnixMilli(), Index: indexes}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getGenres(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.withTimeout(r, 10)
	defer cancel()
	genres, _ := s.catalog.ListGenres(ctx, 200)
	items := make([]subsonic.Genre, 0, len(genres))
	for _, g := range genres {
		items = append(items, subsonic.Genre{Value: g})
	}
	payload := &subsonic.PayloadUnion{Genres: &subsonic.Genres{Genre: items}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getScanStatus(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{
		ScanStatus: &subsonic.ScanStatus{Scanning: false, Count: 0},
	}))
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	if username == "" {
		username = s.authCreds.Username
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{
		User: &subsonic.User{
			Username:          username,
			ScrobblingEnabled: true,
			DownloadRole:      true,
			UploadRole:        false,
			PlaylistRole:      true,
			CoverArtRole:      true,
			CommentRole:       false,
			PodcastRole:       false,
			StreamRole:        true,
			JukeboxRole:       false,
			ShareRole:         true,
			SettingsRole:      false,
			AdminRole:         false,
		},
	}))
}

func (s *Server) getArtists(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()

	artists, err := s.catalog.ListArtists(ctx, 5000)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list artists"))
		return
	}

	indexMap := map[string][]subsonic.Artist{}
	for _, a := range artists {
		name := strings.TrimSpace(a.Name)
		if name == "" {
			continue
		}
		bucket := strings.ToUpper(string([]rune(name)[0]))
		if bucket < "A" || bucket > "Z" {
			bucket = "#"
		}
		indexMap[bucket] = append(indexMap[bucket], subsonic.Artist{
			ID:             s.artistCoverArtID(a.ID),
			Name:           a.Name,
			CoverArt:       s.artistCoverArtID(a.ID),
			ArtistImageURL: a.CoverArtURL,
			AlbumCount:     1,
		})
	}

	keys := make([]string, 0, len(indexMap))
	for k := range indexMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	indexes := make([]subsonic.Index, 0, len(keys))
	for _, k := range keys {
		indexes = append(indexes, subsonic.Index{Name: k, Artists: indexMap[k]})
	}

	payload := &subsonic.PayloadUnion{
		Artists: &subsonic.Artists{
			IgnoredArticles: "The El La Los Las Le Les",
			Index:           indexes,
		},
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getMusicDirectory(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(10, "id is required"))
		return
	}
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()

	// Subsonic root folder handling.
	if id == "1" {
		artists, err := s.catalog.ListArtists(ctx, 5000)
		if err != nil {
			subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list root directory"))
			return
		}
		dir := subsonic.Directory{ID: "1", Name: "Dhwani"}
		for _, a := range artists {
			dir.Children = append(dir.Children, subsonic.DirectoryChild{
				ID:       s.artistCoverArtID(a.ID),
				Parent:   "1",
				IsDir:    true,
				Title:    a.Name,
				Artist:   a.Name,
				ArtistID: s.artistCoverArtID(a.ID),
				CoverArt: s.artistCoverArtID(a.ID),
			})
		}
		payload := &subsonic.PayloadUnion{MusicDirectory: &subsonic.MusicDirectory{Directory: dir}}
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
		return
	}

	dir := subsonic.Directory{ID: id, Name: id}
	kind, rawID := parseCoverArtID(id)
	if kind == "artist" || kind == "track" {
		if artist, err := s.catalog.GetArtist(ctx, rawID); err == nil {
			dir.ID = s.artistCoverArtID(artist.ID)
			dir.Name = artist.Name
			albums, _ := s.catalog.ListAlbumsByArtist(ctx, rawID, 500, 0)
			for _, a := range albums {
				dir.Children = append(dir.Children, subsonic.DirectoryChild{
					ID:       s.albumCoverArtID(a.ID),
					Parent:   s.artistCoverArtID(artist.ID),
					IsDir:    true,
					Title:    a.Name,
					Artist:   a.Artist,
					ArtistID: s.artistCoverArtID(a.ArtistID),
					CoverArt: s.albumCoverArtID(a.ID),
				})
			}
			payload := &subsonic.PayloadUnion{MusicDirectory: &subsonic.MusicDirectory{Directory: dir}}
			subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
			return
		}
	}
	if kind == "album" || kind == "track" {
		album, err := s.catalog.GetAlbum(ctx, rawID)
		if err != nil {
			subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(70, "directory not found"))
			return
		}
		dir.Name = album.Name
		dir.ID = s.albumCoverArtID(album.ID)
		dir.Parent = s.artistCoverArtID(album.ArtistID)
		tracks, _ := s.catalog.ListTracksByAlbum(ctx, album.ID, 1000, 0)
		for _, t := range tracks {
			dir.Children = append(dir.Children, s.toDirectoryChild(t))
		}
		payload := &subsonic.PayloadUnion{MusicDirectory: &subsonic.MusicDirectory{Directory: dir}}
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
		return
	}

	if artist, err := s.catalog.GetArtist(ctx, rawID); err == nil {
		dir.ID = s.artistCoverArtID(artist.ID)
		dir.Name = artist.Name
		albums, _ := s.catalog.ListAlbumsByArtist(ctx, rawID, 500, 0)
		for _, a := range albums {
			dir.Children = append(dir.Children, subsonic.DirectoryChild{
				ID:       s.albumCoverArtID(a.ID),
				Parent:   s.artistCoverArtID(artist.ID),
				IsDir:    true,
				Title:    a.Name,
				Artist:   a.Artist,
				ArtistID: s.artistCoverArtID(a.ArtistID),
				CoverArt: s.albumCoverArtID(a.ID),
			})
		}
		payload := &subsonic.PayloadUnion{MusicDirectory: &subsonic.MusicDirectory{Directory: dir}}
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
		return
	}
	album, err := s.catalog.GetAlbum(ctx, rawID)
	if err != nil {
		subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(70, "directory not found"))
		return
	}
	dir.Name = album.Name
	dir.ID = s.albumCoverArtID(album.ID)
	dir.Parent = s.artistCoverArtID(album.ArtistID)
	tracks, _ := s.catalog.ListTracksByAlbum(ctx, album.ID, 1000, 0)
	for _, t := range tracks {
		dir.Children = append(dir.Children, s.toDirectoryChild(t))
	}
	payload := &subsonic.PayloadUnion{MusicDirectory: &subsonic.MusicDirectory{Directory: dir}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getAlbumList(w http.ResponseWriter, r *http.Request) {
	s.writeAlbumList(r, w, false)
}

func (s *Server) getAlbumList2(w http.ResponseWriter, r *http.Request) {
	s.writeAlbumList(r, w, true)
}

func (s *Server) writeAlbumList(r *http.Request, w http.ResponseWriter, v2 bool) {
	limit, offset := paging(r, 50)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	albums, err := s.catalog.ListAlbums(ctx, limit, offset)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list albums"))
		return
	}
	items := make([]subsonic.Album, 0, len(albums))
	for _, a := range albums {
		items = append(items, subsonic.Album{
			ID:            s.albumCoverArtID(a.ID),
			Name:          a.Name,
			Artist:        a.Artist,
			DisplayArtist: a.Artist,
			ArtistID:      s.artistCoverArtID(a.ArtistID),
			CoverArt:      s.albumCoverArtID(a.ID),
			SongCount:     a.SongCount,
			Duration:      a.DurationSec,
			Year:          a.Year,
		})
	}
	if v2 {
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{AlbumList2: &subsonic.AlbumList2{Albums: items}}))
		return
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{AlbumList: &subsonic.AlbumList{Albums: items}}))
}

func (s *Server) getArtistInfo2(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	info := &subsonic.ArtistInfo2{SimilarArtist: []subsonic.Artist{}}
	if id != "" {
		id = rawArtistID(id)
		ctx, cancel := s.withTimeout(r, 10)
		defer cancel()
		if artist, err := s.catalog.GetArtist(ctx, id); err == nil {
			info.SmallImageURL = artist.CoverArtURL
			info.MediumImageURL = artist.CoverArtURL
			info.LargeImageURL = artist.CoverArtURL
		}
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{ArtistInfo2: info}))
}

func (s *Server) getArtistInfo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	info := &subsonic.ArtistInfo{SimilarArtist: []subsonic.Artist{}}
	if id != "" {
		id = rawArtistID(id)
		ctx, cancel := s.withTimeout(r, 10)
		defer cancel()
		if artist, err := s.catalog.GetArtist(ctx, id); err == nil {
			info.SmallImageURL = artist.CoverArtURL
			info.MediumImageURL = artist.CoverArtURL
			info.LargeImageURL = artist.CoverArtURL
		}
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{ArtistInfo: info}))
}

func (s *Server) getAlbumInfo(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{AlbumInfo: &subsonic.AlbumInfo{}}))
}

func (s *Server) getAlbumInfo2(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{AlbumInfo2: &subsonic.AlbumInfo{}}))
}

func (s *Server) getPlaylists(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{Playlists: &subsonic.Playlists{Items: []subsonic.Playlist{}}}))
}

func (s *Server) getPlaylist(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	p := &subsonic.Playlist{ID: id, Name: "Dhwani Playlist", Entries: []subsonic.Song{}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{Playlist: p}))
}

func (s *Server) getStarred(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r, 200)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	tracks, err := s.catalog.ListStarredTracks(ctx, limit, offset)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list starred songs"))
		return
	}
	artists, aErr := s.catalog.ListStarredArtists(ctx, limit, offset)
	albums, alErr := s.catalog.ListStarredAlbums(ctx, limit, offset)
	if aErr != nil || alErr != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list starred items"))
		return
	}
	subArtists := make([]subsonic.Artist, 0, len(artists))
	for _, a := range artists {
		subArtists = append(subArtists, subsonic.Artist{
			ID:             s.artistCoverArtID(a.ID),
			Name:           a.Name,
			CoverArt:       s.artistCoverArtID(a.ID),
			ArtistImageURL: a.CoverArtURL,
		})
	}
	subAlbums := make([]subsonic.Album, 0, len(albums))
	for _, a := range albums {
		subAlbums = append(subAlbums, subsonic.Album{
			ID:            s.albumCoverArtID(a.ID),
			Name:          a.Name,
			Artist:        a.Artist,
			DisplayArtist: a.Artist,
			ArtistID:      s.artistCoverArtID(a.ArtistID),
			CoverArt:      s.albumCoverArtID(a.ID),
			SongCount:     a.SongCount,
			Duration:      a.DurationSec,
			Year:          a.Year,
		})
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{
		Starred: &subsonic.Starred{
			Artists: subArtists,
			Albums:  subAlbums,
			Songs:   s.tracksToSongs(tracks),
		},
	}))
}

func (s *Server) getStarred2(w http.ResponseWriter, r *http.Request) {
	limit, offset := paging(r, 200)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()

	artists, aErr := s.catalog.ListStarredArtists(ctx, limit, offset)
	albums, alErr := s.catalog.ListStarredAlbums(ctx, limit, offset)
	tracks, tErr := s.catalog.ListStarredTracks(ctx, limit, offset)
	if aErr != nil || alErr != nil || tErr != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list starred items"))
		return
	}

	subArtists := make([]subsonic.Artist, 0, len(artists))
	for _, a := range artists {
		subArtists = append(subArtists, subsonic.Artist{
			ID:             s.artistCoverArtID(a.ID),
			Name:           a.Name,
			CoverArt:       s.artistCoverArtID(a.ID),
			ArtistImageURL: a.CoverArtURL,
		})
	}
	subAlbums := make([]subsonic.Album, 0, len(albums))
	for _, a := range albums {
		subAlbums = append(subAlbums, subsonic.Album{
			ID:            s.albumCoverArtID(a.ID),
			Name:          a.Name,
			Artist:        a.Artist,
			DisplayArtist: a.Artist,
			ArtistID:      s.artistCoverArtID(a.ArtistID),
			CoverArt:      s.albumCoverArtID(a.ID),
			SongCount:     a.SongCount,
			Duration:      a.DurationSec,
			Year:          a.Year,
		})
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{
		Starred2: &subsonic.Starred2{
			Artists: subArtists,
			Albums:  subAlbums,
			Songs:   s.tracksToSongs(tracks),
		},
	}))
}

func (s *Server) getNowPlaying(w http.ResponseWriter, r *http.Request) {
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{NowPlaying: &subsonic.NowPlaying{Entries: []subsonic.Song{}}}))
}

func (s *Server) getRandomSongs(w http.ResponseWriter, r *http.Request) {
	limit := limitFromSize(r, 20)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	tracks, err := s.catalog.RandomTracks(ctx, limit)
	if err != nil {
		s.logger.Warn("getRandomSongs failed", "err", err)
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list random songs"))
		return
	}
	songs := make([]subsonic.Song, 0, len(tracks))
	for _, t := range tracks {
		songs = append(songs, s.toSubsonicSong(t))
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{RandomSongs: &subsonic.RandomSongs{Songs: songs}}))
}

func (s *Server) getSongsByGenre(w http.ResponseWriter, r *http.Request) {
	genre := strings.TrimSpace(r.URL.Query().Get("genre"))
	limit, offset := paging(r, 50)
	if genre == "" {
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{SongsByGenre: &subsonic.SongsByGenre{Songs: []subsonic.Song{}}}))
		return
	}
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	tracks, err := s.catalog.ListTracksByGenre(ctx, genre, limit, offset)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not list songs by genre"))
		return
	}
	songs := make([]subsonic.Song, 0, len(tracks))
	for _, t := range tracks {
		songs = append(songs, s.toSubsonicSong(t))
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(&subsonic.PayloadUnion{SongsByGenre: &subsonic.SongsByGenre{Songs: songs}}))
}

func (s *Server) compatNotImplemented(w http.ResponseWriter, r *http.Request) {
	client := strings.TrimSpace(r.URL.Query().Get("c"))
	if client == "" {
		client = "-"
	}
	s.logger.Debug("compat fallback endpoint hit", "path", r.URL.Path, "client", client)
	subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(0, "endpoint not found"))
}

func (s *Server) search2(w http.ResponseWriter, r *http.Request) {
	s.searchCommon(w, r, true)
}

func (s *Server) search3(w http.ResponseWriter, r *http.Request) {
	s.searchCommon(w, r, false)
}

func (s *Server) searchCommon(w http.ResponseWriter, r *http.Request, useSearchResult2 bool) {
	query := strings.TrimSpace(r.URL.Query().Get("query"))

	artistCount, artistOffset := subsonicCountOffset(r, "artistCount", "artistOffset", 20)
	albumCount, albumOffset := subsonicCountOffset(r, "albumCount", "albumOffset", 20)
	songCount, songOffset := subsonicCountOffset(r, "songCount", "songOffset", 20)

	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()

	res := model.SearchResult{}
	if query == "" {
		// Arpeggi uses empty-query search3 as a "library songs" listing call.
		if songCount > 0 {
			tracks, err := s.catalog.ListTracks(ctx, songCount, songOffset)
			if err == nil {
				res.Tracks = tracks
			}
		}
		if albumCount > 0 {
			albums, err := s.catalog.ListAlbums(ctx, albumCount, albumOffset)
			if err == nil {
				res.Albums = albums
			}
		}
		if artistCount > 0 {
			artists, err := s.catalog.ListArtists(ctx, artistCount+artistOffset)
			if err == nil {
				res.Artists = artists
			}
		}
	} else {
		searchLimit := maxInt(artistCount+artistOffset, albumCount+albumOffset, songCount+songOffset)
		if searchLimit <= 0 {
			searchLimit = 20
		}
		// Hard cap to keep live-search latency stable for mobile clients.
		if searchLimit > 100 {
			searchLimit = 100
		}
