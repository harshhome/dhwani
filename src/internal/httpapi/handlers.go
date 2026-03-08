package httpapi

import (
	"context"
	"errors"
	"fmt"
	_ "image/png"
	"io"
	"net/http"
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
		var err error
		res, err = s.catalog.Search(ctx, query, searchLimit)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				s.logger.Info("search canceled", "query", query)
				payload := &subsonic.PayloadUnion{}
				searchRes := &subsonic.SearchResult{
					Artists: []subsonic.Artist{},
					Albums:  []subsonic.Album{},
					Songs:   []subsonic.Song{},
				}
				if useSearchResult2 {
					payload.SearchResult2 = searchRes
				} else {
					payload.SearchResult = searchRes
				}
				subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
				return
			}
			subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "search failed"))
			return
		}
		res.Artists = sliceArtists(res.Artists, artistOffset, artistCount)
		res.Albums = sliceAlbums(res.Albums, albumOffset, albumCount)
		res.Tracks = sliceTracks(res.Tracks, songOffset, songCount)
		s.catalog.RememberTracks(res.Tracks)
	}
	// Apply offsets for local list mode.
	if query == "" {
		res.Artists = sliceArtists(res.Artists, artistOffset, artistCount)
		res.Albums = sliceAlbums(res.Albums, albumOffset, albumCount)
		res.Tracks = sliceTracks(res.Tracks, songOffset, songCount)
	}

	artists := make([]subsonic.Artist, 0, len(res.Artists))
	for _, a := range res.Artists {
		artists = append(artists, subsonic.Artist{
			ID:             s.artistCoverArtID(a.ID),
			Name:           a.Name,
			CoverArt:       s.artistCoverArtID(a.ID),
			ArtistImageURL: a.CoverArtURL,
		})
	}
	albums := make([]subsonic.Album, 0, len(res.Albums))
	for _, a := range res.Albums {
		albums = append(albums, subsonic.Album{
			ID:            s.albumCoverArtID(a.ID),
			Name:          a.Name,
			Artist:        a.Artist,
			DisplayArtist: a.Artist,
			ArtistID:      s.artistCoverArtID(a.ArtistID),
			Year:          a.Year,
			CoverArt:      s.albumCoverArtID(a.ID),
		})
	}
	songs := make([]subsonic.Song, 0, len(res.Tracks))
	for _, t := range res.Tracks {
		songs = append(songs, s.toSubsonicSong(t))
	}

	payload := &subsonic.PayloadUnion{}
	searchRes := &subsonic.SearchResult{Artists: artists, Albums: albums, Songs: songs}
	if useSearchResult2 {
		payload.SearchResult2 = searchRes
	} else {
		payload.SearchResult = searchRes
	}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getSong(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(10, "id is required"))
		return
	}

	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()

	track, err := s.catalog.GetTrack(ctx, id)
	if err != nil {
		cached, cErr := s.catalog.GetCachedTrackAny(ctx, id)
		if cErr != nil {
			if service.IsNotFound(err) {
				subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(70, "song not found"))
				return
			}
			subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(70, "could not resolve song"))
			return
		}
		track = cached
	}
	if strings.TrimSpace(track.Title) == "" || strings.TrimSpace(track.Artist) == "" || strings.TrimSpace(track.Album) == "" {
		if cached, cErr := s.catalog.GetCachedTrackAny(ctx, id); cErr == nil {
			if strings.TrimSpace(track.Title) == "" {
				track.Title = cached.Title
			}
			if strings.TrimSpace(track.Artist) == "" {
				track.Artist = cached.Artist
			}
			if strings.TrimSpace(track.Album) == "" {
				track.Album = cached.Album
			}
			if strings.TrimSpace(track.ArtistID) == "" {
				track.ArtistID = cached.ArtistID
			}
			if strings.TrimSpace(track.AlbumID) == "" {
				track.AlbumID = cached.AlbumID
			}
			if strings.TrimSpace(track.CoverArtURL) == "" {
				track.CoverArtURL = cached.CoverArtURL
			}
		}
	}
	if strings.TrimSpace(track.Title) == "" {
		track.Title = "Track " + strings.TrimSpace(id)
	}
	if strings.TrimSpace(track.Artist) == "" {
		track.Artist = "Unknown Artist"
	}
	if strings.TrimSpace(track.Album) == "" {
		track.Album = "Unknown Album"
	}
	if strings.TrimSpace(track.ArtistID) == "" {
		track.ArtistID = "unknown-artist"
	}
	if strings.TrimSpace(track.AlbumID) == "" {
		track.AlbumID = "unknown-album"
	}
	s.catalog.RememberTracks([]model.Track{track})

	albumArtist, albumArtistID := track.Artist, track.ArtistID
	payload := &subsonic.PayloadUnion{Song: ptrSong(s.toSubsonicSongWithAlbumArtist(track, albumArtist, albumArtistID))}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getArtist(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(10, "id is required"))
		return
	}
	id = rawArtistID(id)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	artist, err := s.catalog.GetArtist(ctx, id)
	if err != nil {
		cached, cErr := s.catalog.GetCachedArtist(ctx, id)
		if cErr != nil {
			if service.IsNotFound(err) {
				subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(70, "artist not found"))
				return
			}
			subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(70, "could not resolve artist"))
			return
		}
		artist = cached
	}
	albums, err := s.catalog.GetArtistAlbumsLive(ctx, id, 500, 0)
	if err != nil {
		// Fallback to local cache for partially known artists.
		albums, _ = s.catalog.ListAlbumsByArtist(ctx, id, 500, 0)
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
			Year:          a.Year,
			SongCount:     a.SongCount,
			Duration:      a.DurationSec,
		})
	}
	payload := &subsonic.PayloadUnion{Artist: &subsonic.Artist{
		ID:             s.artistCoverArtID(artist.ID),
		Name:           artist.Name,
		CoverArt:       s.artistCoverArtID(artist.ID),
		ArtistImageURL: artist.CoverArtURL,
		AlbumCount:     len(subAlbums),
		Albums:         subAlbums,
	}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getAlbum(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(10, "id is required"))
		return
	}
	id = rawAlbumID(id)
	ctx, cancel := s.withTimeout(r, 20)
	defer cancel()
	album, err := s.catalog.GetAlbum(ctx, id)
	if err != nil {
		cached, cErr := s.catalog.GetCachedAlbum(ctx, id)
		if cErr != nil {
			if service.IsNotFound(err) {
				subsonic.Write(w, r, http.StatusNotFound, subsonic.NewError(70, "album not found"))
				return
			}
			subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(70, "could not resolve album"))
			return
		}
		album = cached
	}
	tracks, err := s.catalog.GetAlbumTracksLive(ctx, id, 1000, 0)
	if err != nil {
		// Fallback to locally cached tracks if upstream fails.
		tracks, _ = s.catalog.ListTracksByAlbum(ctx, id, 1000, 0)
	}
	if len(tracks) == 0 {
		tracks, _ = s.catalog.ListTracksByAlbum(ctx, id, 1000, 0)
	}
	// Some provider album payloads omit artist linkage; backfill from tracks.
	if strings.TrimSpace(album.Artist) == "" || strings.TrimSpace(album.ArtistID) == "" {
		for _, t := range tracks {
			if strings.TrimSpace(album.Artist) == "" && strings.TrimSpace(t.Artist) != "" {
				album.Artist = t.Artist
			}
			if strings.TrimSpace(album.ArtistID) == "" && strings.TrimSpace(t.ArtistID) != "" {
				album.ArtistID = t.ArtistID
			}
			if strings.TrimSpace(album.Artist) != "" && strings.TrimSpace(album.ArtistID) != "" {
				break
			}
		}
	}
	// Keep recently viewed album tracks hot so star-ingestion can reuse rich metadata.
	s.catalog.RememberTracks(tracks)
	songs := make([]subsonic.Song, 0, len(tracks))
	for _, t := range tracks {
		songs = append(songs, s.toSubsonicSongWithAlbumArtist(t, album.Artist, album.ArtistID))
	}
	totalDuration := 0
	for _, t := range tracks {
		totalDuration += t.DurationSec
	}
	payload := &subsonic.PayloadUnion{Album: &subsonic.Album{
		ID:            s.albumCoverArtID(album.ID),
		Name:          album.Name,
		Artist:        album.Artist,
		DisplayArtist: album.Artist,
		ArtistID:      s.artistCoverArtID(album.ArtistID),
		Year:          album.Year,
		CoverArt:      s.albumCoverArtID(album.ID),
		SongCount:     len(songs),
		Duration:      totalDuration,
		Songs:         songs,
	}}
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(payload))
}

func (s *Server) getCoverArt(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writePlaceholder(w, s.placeHolder)
		return
	}
	id = strings.TrimSpace(id)
	ctx, cancel := s.withTimeout(r, 4)
	defer cancel()

	kind, rawID := parseCoverArtID(id)

	if kind == "artist" {
		// Keep prefixed artist IDs type-safe: only resolve artist artwork.
		if artist, err := s.catalog.GetCachedArtistAny(ctx, rawID); err == nil && strings.TrimSpace(artist.CoverArtURL) != "" {
			s.proxyBinary(w, r, artist.CoverArtURL)
			return
		}
		artist, err := s.catalog.GetArtist(ctx, rawID)
		if err == nil && artist.CoverArtURL != "" {
			s.proxyBinary(w, r, artist.CoverArtURL)
			return
		}
		writePlaceholder(w, s.placeHolder)
		return
	}

	if kind == "album" {
		// Keep prefixed album IDs type-safe: only resolve album artwork.
		if album, err := s.catalog.GetCachedAlbumAny(ctx, rawID); err == nil && strings.TrimSpace(album.CoverArtURL) != "" {
			s.proxyBinary(w, r, album.CoverArtURL)
			return
		}
		album, err := s.catalog.GetAlbum(ctx, rawID)
		if err == nil && album.CoverArtURL != "" {
			s.proxyBinary(w, r, album.CoverArtURL)
			return
		}
		writePlaceholder(w, s.placeHolder)
		return
	}

	// Raw track IDs can use any cached source as fast path.
	if u := s.catalog.ResolveCoverArtURL(ctx, rawID); u != "" {
		s.proxyBinary(w, r, u)
		return
	}

	// Track IDs are raw numeric IDs. Lookup track first, then album fallback.
	track, err := s.catalog.GetTrack(ctx, rawID)
	if err == nil && track.CoverArtURL != "" {
		s.proxyBinary(w, r, track.CoverArtURL)
		return
	}

	if strings.TrimSpace(track.AlbumID) != "" {
		if album, aErr := s.catalog.GetAlbum(ctx, track.AlbumID); aErr == nil && album.CoverArtURL != "" {
			s.proxyBinary(w, r, album.CoverArtURL)
			return
		}
	}
	if album, err := s.catalog.GetAlbum(ctx, rawID); err == nil && album.CoverArtURL != "" {
		s.proxyBinary(w, r, album.CoverArtURL)
		return
	}

	writePlaceholder(w, s.placeHolder)
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		subsonic.Write(w, r, http.StatusBadRequest, subsonic.NewError(10, "id is required"))
		return
	}

	ctx, cancel := s.withTimeout(r, 45)
	defer cancel()
	res, err := s.catalog.ResolveStream(ctx, id)
	if err != nil {
		s.logger.Warn("stream resolution failed", "id", id, "err", err)
		if errors.Is(err, provider.ErrNoFullStream) {
			subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "no FULL stream available"))
			return
		}
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not resolve stream"))
		return
	}
	if s.ingestOnStream {
		go func(trackID string) {
			bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s.catalog.RecordPlayedTrack(bg, trackID)
		}(id)
	}

	if strings.EqualFold(res.ManifestMIME, "application/dash+xml") {
		s.logger.Info("stream start", "id", id, "provider", res.Provider, "type", "dash", "range", r.Header.Get("Range"))
		if err := s.streamDASH(w, r, id, res); err != nil {
			if errors.Is(err, errInvalidRange) {
				subsonic.Write(w, r, http.StatusRequestedRangeNotSatisfiable, subsonic.NewError(10, "invalid range"))
				return
			}
			s.logger.Warn("dash stream failed", "id", id, "provider", res.Provider, "err", err)
			subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "could not serve DASH stream"))
			return
		}
		s.logger.Info("stream finished", "id", id, "provider", res.Provider, "type", "dash")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, res.MediaURL, nil)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "invalid upstream stream url"))
		return
	}
	if rg := r.Header.Get("Range"); rg != "" {
		req.Header.Set("Range", rg)
	}

	s.logger.Info("stream start", "id", id, "provider", res.Provider, "range", r.Header.Get("Range"))
	up, err := s.httpClient.Do(req)
	if err != nil {
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, "upstream stream failed"))
		return
	}
	defer up.Body.Close()

	if up.StatusCode >= 400 {
		s.logger.Warn("upstream stream status", "status", up.StatusCode, "id", id)
		subsonic.Write(w, r, http.StatusBadGateway, subsonic.NewError(70, fmt.Sprintf("upstream returned %d", up.StatusCode)))
		return
	}

	passthroughHeaders(up.Header, w.Header())
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", res.ManifestMIME)
	}
	w.WriteHeader(up.StatusCode)

	_, copyErr := io.Copy(w, up.Body)
	if copyErr != nil && !errors.Is(copyErr, io.EOF) && r.Context().Err() == nil {
		s.logger.Warn("stream copy error", "id", id, "err", copyErr)
		return
	}
	s.logger.Info("stream finished", "id", id, "status", up.StatusCode)
}


func (s *Server) proxyBinary(w http.ResponseWriter, r *http.Request, target string) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		writePlaceholder(w, s.placeHolder)
		return
	}
	resp, err := s.httpClient.Do(req)
	if err != nil || resp.StatusCode >= 400 {
		if resp != nil {
			resp.Body.Close()
		}
		writePlaceholder(w, s.placeHolder)
		return
	}
	defer resp.Body.Close()
	passthroughHeaders(resp.Header, w.Header())
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func passthroughHeaders(src, dst http.Header) {
	for _, k := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified", "Cache-Control"} {
		if v := src.Get(k); v != "" {
			dst.Set(k, v)
		}
	}
}

func writePlaceholder(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func ptrSong(v subsonic.Song) *subsonic.Song { return &v }

func (s *Server) toSubsonicSong(t model.Track) subsonic.Song {
	return s.toSubsonicSongWithAlbumArtist(t, t.Artist, t.ArtistID)
}

func (s *Server) toSubsonicSongWithAlbumArtist(t model.Track, albumArtist string, albumArtistID string) subsonic.Song {
	displayArtist := strings.TrimSpace(t.DisplayArtist)
	if displayArtist == "" {
		displayArtist = t.Artist
	}
	songArtists := make([]subsonic.SongArtist, 0, len(t.Artists))
	for _, a := range t.Artists {
		name := strings.TrimSpace(a.Name)
		id := strings.TrimSpace(a.ID)
		if name == "" && id == "" {
			continue
		}
		songArtists = append(songArtists, subsonic.SongArtist{
			ID:   s.artistCoverArtID(id),
			Name: name,
		})
	}
	if strings.TrimSpace(albumArtist) == "" {
		albumArtist = t.Artist
	}
	if strings.TrimSpace(albumArtistID) == "" {
		albumArtistID = t.ArtistID
	}
	albumArtists := []subsonic.SongArtist{}
	if strings.TrimSpace(albumArtist) != "" || strings.TrimSpace(albumArtistID) != "" {
		albumArtists = append(albumArtists, subsonic.SongArtist{
			ID:   s.artistCoverArtID(albumArtistID),
			Name: albumArtist,
		})
	}
	return subsonic.Song{
		ID:                 s.displayID(t.ID),
		Parent:             s.albumCoverArtID(t.AlbumID),
		Title:              t.Title,
		Album:              t.Album,
		Artist:             displayArtist,
		DisplayArtist:      displayArtist,
		Artists:            songArtists,
		AlbumArtists:       albumArtists,
		DisplayAlbumArtist: albumArtist,
		Track:              t.TrackNumber,
		Duration:           t.DurationSec,
		BitRate:            t.BitRate,
		ContentType:        t.ContentType,
		CoverArt:           s.trackCoverArtID(t.ID),
		ArtistID:           s.artistCoverArtID(t.ArtistID),
		AlbumID:            s.albumCoverArtID(t.AlbumID),
		DiscNumber:         t.DiscNumber,
		Type:               "music",
	}
}

func (s *Server) toDirectoryChild(t model.Track) subsonic.DirectoryChild {
	displayArtist := strings.TrimSpace(t.DisplayArtist)
	if displayArtist == "" {
		displayArtist = t.Artist
	}
	return subsonic.DirectoryChild{
		ID:          s.displayID(t.ID),
		Parent:      s.albumCoverArtID(t.AlbumID),
		IsDir:       false,
		Title:       t.Title,
		Album:       t.Album,
		Artist:      displayArtist,
		Track:       t.TrackNumber,
		CoverArt:    s.trackCoverArtID(t.ID),
		Duration:    t.DurationSec,
		BitRate:     t.BitRate,
		ContentType: t.ContentType,
		ArtistID:    s.artistCoverArtID(t.ArtistID),
		AlbumID:     s.albumCoverArtID(t.AlbumID),
	}
}

func (s *Server) star(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	trackIDs, albumIDs, artistIDs := parseStarTargets(r.Form["id"], r.Form["albumId"], r.Form["artistId"])
	reqStart := time.Now()
	s.logger.Info("star request accepted",
		"track_ids", len(trackIDs),
		"album_ids", len(albumIDs),
		"artist_ids", len(artistIDs),
		"ingest_enabled", s.ingestOnStar,
	)

	ctx, cancel := s.withTimeout(r, 10)
	defer cancel()
	for _, id := range trackIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.catalog.StarTrack(ctx, id); err != nil {
			s.logger.Warn("star track persist failed", "id", id, "err", err)
		}
	}
	for _, id := range albumIDs {
		id = rawAlbumID(id)
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.catalog.StarAlbum(ctx, id); err != nil {
			s.logger.Warn("star album persist failed", "id", id, "err", err)
		}
	}
	for _, id := range artistIDs {
		id = rawArtistID(id)
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.catalog.StarArtist(ctx, id); err != nil {
			s.logger.Warn("star artist persist failed", "id", id, "err", err)
		}
	}

	if s.ingestOnStar {
		go func(trackIDs, albumIDs []string) {
			bg, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			ingestStart := time.Now()
			trackOK, albumOK := 0, 0
			for _, id := range trackIDs {
				if strings.TrimSpace(id) == "" {
					continue
				}
				if err := s.catalog.IngestStarredTrack(bg, id); err != nil {
					s.logger.Warn("star track ingest failed", "id", id, "err", err)
				} else {
					trackOK++
				}
			}
			for _, albumID := range albumIDs {
				albumID = rawAlbumID(albumID)
				if strings.TrimSpace(albumID) == "" {
					continue
				}
				if err := s.catalog.IngestStarredAlbum(bg, albumID); err != nil {
					s.logger.Warn("star album ingest failed", "id", albumID, "err", err)
				} else {
					albumOK++
				}
			}
			s.logger.Info("star ingest completed",
				"track_ok", trackOK,
				"album_ok", albumOK,
				"duration_ms", time.Since(ingestStart).Milliseconds(),
			)
		}(trackIDs, albumIDs)
	}

	// Keep star response minimal/fast for client compatibility.
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(nil))
	s.logger.Debug("star response sent", "duration_ms", time.Since(reqStart).Milliseconds())
}

func (s *Server) unstar(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	trackIDs, albumIDs, artistIDs := parseStarTargets(r.Form["id"], r.Form["albumId"], r.Form["artistId"])
	if len(trackIDs) == 0 && len(albumIDs) == 0 && len(artistIDs) == 0 {
		subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(nil))
		return
	}

	ctx, cancel := s.withTimeout(r, 30)
	defer cancel()
	removed := 0
	for _, id := range trackIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if err := s.catalog.UnstarTrack(ctx, id); err != nil {
			s.logger.Warn("unstar track delete failed", "id", id, "err", err)
		} else {
			removed++
		}
	}
	for _, id := range albumIDs {
		id = rawAlbumID(id)
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.catalog.UnstarAlbum(ctx, id); err != nil {
			s.logger.Warn("unstar album delete failed", "id", id, "err", err)
		}
	}
	for _, id := range artistIDs {
		id = rawArtistID(id)
		if strings.TrimSpace(id) == "" {
			continue
		}
		if err := s.catalog.UnstarArtist(ctx, id); err != nil {
			s.logger.Warn("unstar artist delete failed", "id", id, "err", err)
		}
	}
	s.logger.Info("unstar completed", "requested_tracks", len(trackIDs), "removed_tracks", removed, "requested_albums", len(albumIDs), "requested_artists", len(artistIDs))
	subsonic.Write(w, r, http.StatusOK, subsonic.NewSuccess(nil))
}

func parseStarTargets(ids []string, albumIDs []string, artistIDs []string) (tracks []string, albums []string, artists []string) {
	addTrack := func(v string) {
		v = strings.TrimSpace(v)
		if v != "" {
			tracks = append(tracks, v)
		}
	}
	addAlbum := func(v string) {
		v = strings.TrimSpace(rawAlbumID(v))
		if v != "" {
			albums = append(albums, v)
		}
	}
	addArtist := func(v string) {
		v = strings.TrimSpace(rawArtistID(v))
		if v != "" {
			artists = append(artists, v)
		}
	}

	for _, id := range ids {
		kind, raw := parseCoverArtID(id)
		switch kind {
		case "album":
			addAlbum(raw)
		case "artist":
			addArtist(raw)
		default:
			addTrack(id)
		}
	}
	for _, id := range albumIDs {
		addAlbum(id)
	}
	for _, id := range artistIDs {
		addArtist(id)
	}
	return tracks, albums, artists
}


func (s *Server) tracksToSongs(in []model.Track) []subsonic.Song {
	out := make([]subsonic.Song, 0, len(in))
	for _, t := range in {
		out = append(out, s.toSubsonicSong(t))
	}
	return out
}
func (s *Server) displayID(v string) string {
	return strings.TrimSpace(v)
}

func (s *Server) trackCoverArtID(id string) string {
	return s.displayID(id)
}

func (s *Server) artistCoverArtID(id string) string {
	id = s.displayID(id)
	if id == "" {
		return ""
	}
	return "ar-" + id
}

func (s *Server) albumCoverArtID(id string) string {
	id = s.displayID(id)
	if id == "" {
		return ""
	}
	return "al-" + id
}

func parseCoverArtID(id string) (kind string, rawID string) {
	trimmed := strings.TrimSpace(id)
	switch {
	case strings.HasPrefix(trimmed, "ar-"):
		return "artist", strings.TrimPrefix(trimmed, "ar-")
	case strings.HasPrefix(trimmed, "al-"):
		return "album", strings.TrimPrefix(trimmed, "al-")
	default:
		return "track", trimmed
	}
}

func rawArtistID(id string) string {
	kind, rawID := parseCoverArtID(id)
	if kind == "artist" {
		return rawID
	}
	return strings.TrimSpace(id)
}

func rawAlbumID(id string) string {
	kind, rawID := parseCoverArtID(id)
	if kind == "album" {
		return rawID
	}
	return strings.TrimSpace(id)
}

func paging(r *http.Request, def int) (limit int, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("size"))
	if limit <= 0 {
		limit = def
	}
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	return
}

func limitFromSize(r *http.Request, def int) int {
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		return def
	}
	return size
}

func subsonicCountOffset(r *http.Request, countKey, offsetKey string, defCount int) (count int, offset int) {
	rawCount := strings.TrimSpace(r.URL.Query().Get(countKey))
	if rawCount == "" {
		count = defCount
	} else {
		count, _ = strconv.Atoi(rawCount)
		if count < 0 {
			count = 0
		}
	}
	if count > 200 {
		count = 200
	}
	offset, _ = strconv.Atoi(r.URL.Query().Get(offsetKey))
	if offset < 0 {
		offset = 0
	}
	return count, offset
}

func maxInt(v ...int) int {
	m := 0
	for _, n := range v {
		if n > m {
			m = n
		}
	}
	return m
}

func sliceArtists(in []model.Artist, offset, count int) []model.Artist {
	if offset >= len(in) {
		return []model.Artist{}
	}
	end := offset + count
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}

func sliceAlbums(in []model.Album, offset, count int) []model.Album {
	if offset >= len(in) {
		return []model.Album{}
	}
	end := offset + count
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}

func sliceTracks(in []model.Track, offset, count int) []model.Track {
	if offset >= len(in) {
		return []model.Track{}
	}
	end := offset + count
	if end > len(in) {
		end = len(in)
	}
	return in[offset:end]
}
