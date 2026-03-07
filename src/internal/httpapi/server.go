package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"dhwani/internal/auth"
	"dhwani/internal/service"
	"dhwani/internal/subsonic"
)

type Server struct {
	logger                *slog.Logger
	catalog               *service.CatalogService
	authCreds             auth.Credentials
	httpClient            *http.Client
	enableJSON            bool
	placeHolder           []byte
	ingestOnStream        bool
	ingestOnStar          bool
	dashCacheMu           sync.RWMutex
	dashCache             map[string]dashByteMap
}

func NewServer(logger *slog.Logger, catalog *service.CatalogService, authCreds auth.Credentials, streamClient *http.Client, enableJSONResponses bool, ingestOnStream bool, ingestOnStar bool) *Server {
	if streamClient == nil {
		streamClient = &http.Client{Timeout: 0}
	}
	return &Server{
		logger:                logger,
		catalog:               catalog,
		authCreds:             authCreds,
		httpClient:            streamClient,
		enableJSON:            enableJSONResponses,
		placeHolder:           onePixelPNG(),
		ingestOnStream:        ingestOnStream,
		ingestOnStar:          ingestOnStar,
		dashCache:             make(map[string]dashByteMap),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.requestLogMiddleware)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	r.Route("/rest", func(rest chi.Router) {
		rest.Group(func(gr chi.Router) {
			gr.Use(s.jsonFormatMiddleware)
			gr.Use(s.authMiddleware)
			gr.Get("/ping", s.ping)
			gr.Get("/ping.view", s.ping)
			gr.Get("/getLicense", s.getLicense)
			gr.Get("/getLicense.view", s.getLicense)
			gr.Get("/getMusicFolders", s.getMusicFolders)
			gr.Get("/getMusicFolders.view", s.getMusicFolders)
			gr.Get("/getIndexes", s.getIndexes)
			gr.Get("/getIndexes.view", s.getIndexes)
			gr.Get("/getArtists", s.getArtists)
			gr.Get("/getArtists.view", s.getArtists)
			gr.Get("/getMusicDirectory", s.getMusicDirectory)
			gr.Get("/getMusicDirectory.view", s.getMusicDirectory)
			gr.Get("/getAlbumList", s.getAlbumList)
			gr.Get("/getAlbumList.view", s.getAlbumList)
			gr.Get("/getAlbumList2", s.getAlbumList2)
			gr.Get("/getAlbumList2.view", s.getAlbumList2)
			gr.Get("/getArtistInfo2", s.getArtistInfo2)
			gr.Get("/getArtistInfo2.view", s.getArtistInfo2)
			gr.Get("/getArtistInfo", s.getArtistInfo)
			gr.Get("/getArtistInfo.view", s.getArtistInfo)
			gr.Get("/getAlbumInfo", s.getAlbumInfo)
			gr.Get("/getAlbumInfo.view", s.getAlbumInfo)
			gr.Get("/getAlbumInfo2", s.getAlbumInfo2)
			gr.Get("/getAlbumInfo2.view", s.getAlbumInfo2)
			gr.Get("/getPlaylists", s.getPlaylists)
			gr.Get("/getPlaylists.view", s.getPlaylists)
			gr.Get("/getPlaylist", s.getPlaylist)
			gr.Get("/getPlaylist.view", s.getPlaylist)
			gr.Get("/getStarred", s.getStarred)
			gr.Get("/getStarred.view", s.getStarred)
			gr.Get("/getStarred2", s.getStarred2)
			gr.Get("/getStarred2.view", s.getStarred2)
			gr.Get("/getNowPlaying", s.getNowPlaying)
			gr.Get("/getNowPlaying.view", s.getNowPlaying)
			gr.Get("/getRandomSongs", s.getRandomSongs)
			gr.Get("/getRandomSongs.view", s.getRandomSongs)
			gr.Get("/getSongsByGenre", s.getSongsByGenre)
			gr.Get("/getSongsByGenre.view", s.getSongsByGenre)
			gr.Get("/getGenres", s.getGenres)
			gr.Get("/getGenres.view", s.getGenres)
			gr.Get("/getScanStatus", s.getScanStatus)
			gr.Get("/getScanStatus.view", s.getScanStatus)
			gr.Get("/getUser", s.getUser)
			gr.Get("/getUser.view", s.getUser)
			gr.Get("/search3", s.search3)
			gr.Get("/search3.view", s.search3)
			gr.Get("/search2", s.search2)
			gr.Get("/search2.view", s.search2)
			gr.Get("/getSong", s.getSong)
			gr.Get("/getSong.view", s.getSong)
			gr.Get("/getArtist", s.getArtist)
			gr.Get("/getArtist.view", s.getArtist)
			gr.Get("/getAlbum", s.getAlbum)
			gr.Get("/getAlbum.view", s.getAlbum)
			gr.Get("/getCoverArt", s.getCoverArt)
			gr.Get("/getCoverArt.view", s.getCoverArt)
			gr.Get("/stream", s.stream)
			gr.Get("/stream.view", s.stream)
			gr.Get("/*", s.compatNotImplemented)
		})
	})

	return r
}

func (s *Server) jsonFormatMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.enableJSON {
			next.ServeHTTP(w, r)
			return
		}
		if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("f")), "json") {
			r2 := r.Clone(r.Context())
			q := r2.URL.Query()
			q.Del("f")
			r2.URL.RawQuery = q.Encode()
			next.ServeHTTP(w, r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/ping" || r.URL.Path == "/rest/ping.view" {
			next.ServeHTTP(w, r)
			return
		}
		if err := auth.ValidateSubsonicAuth(r, s.authCreds); err != nil {
			subsonic.Write(w, r, http.StatusUnauthorized, subsonic.NewError(40, "Wrong username or password"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		duration := time.Since(start)

		client := strings.TrimSpace(r.URL.Query().Get("c"))
		if client == "" {
