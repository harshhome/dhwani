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
