package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
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
	downloadOnStar        bool
	downloadDir           string
	downloadQuality       string
	downloadRetryAttempts int
	ffmpegOnce            sync.Once
	ffmpegPath            string
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
		downloadOnStar:        parseEnvBoolDefault("DHWANI_DOWNLOAD_ON_STAR", false),
		downloadDir:           strings.TrimSpace(os.Getenv("DHWANI_DOWNLOAD_DIR")),
		downloadQuality:       strings.TrimSpace(os.Getenv("DHWANI_DOWNLOAD_QUALITY")),
		downloadRetryAttempts: parseEnvIntDefault("DHWANI_DOWNLOAD_RETRY_ATTEMPTS", 1),
		dashCache:             make(map[string]dashByteMap),
	}
}

func parseEnvBoolDefault(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func parseEnvIntDefault(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return def
	}
	return n
}

func (s *Server) findFFmpegPath() string {
	s.ffmpegOnce.Do(func() {
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			s.ffmpegPath = p
			return
		}
		s.logger.Warn("ffmpeg not found; downloaded audio files will remain untagged")
	})
	return s.ffmpegPath
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
			gr.Get("/getLyrics", s.getLyrics)
			gr.Get("/getLyrics.view", s.getLyrics)
			gr.Get("/getLyricsBySongId", s.getLyricsBySongID)
			gr.Get("/getLyricsBySongId.view", s.getLyricsBySongID)
			gr.Get("/star", s.star)
			gr.Get("/star.view", s.star)
			gr.Post("/star", s.star)
			gr.Post("/star.view", s.star)
			gr.Get("/unstar", s.unstar)
			gr.Get("/unstar.view", s.unstar)
			gr.Post("/unstar", s.unstar)
			gr.Post("/unstar.view", s.unstar)
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
			client = "-"
		}
		id := strings.TrimSpace(r.URL.Query().Get("id"))
		if len(id) > 24 {
			id = id[:24] + "..."
		}
		logLevel := slog.LevelInfo
		if lrw.status == http.StatusNotFound && strings.HasSuffix(r.URL.Path, "/scrobble.view") {
			// Unsupported scrobble polling from some clients can be very noisy.
			logLevel = slog.LevelDebug
		}

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"client", client,
			"id", id,
			"status", lrw.status,
			"bytes", lrw.bytes,
			"duration_ms", duration.Milliseconds(),
			"req_id", middleware.GetReqID(r.Context()),
			"client_ip", r.RemoteAddr,
			"user_agent", r.UserAgent(),
		}
		if s.logger.Enabled(r.Context(), slog.LevelDebug) {
			attrs = append(attrs, "query", summarizeQueryForLog(r.URL.RawQuery))
		}
		s.logger.Log(r.Context(), logLevel, "request", attrs...)
		if duration >= 2*time.Second {
			s.logger.Warn("slow request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", lrw.status,
				"duration_ms", duration.Milliseconds(),
				"req_id", middleware.GetReqID(r.Context()),
			)
		}
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *loggingResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *loggingResponseWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func redactedQueryForLog(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[redacted]"
	}
	for key := range values {
		if isSensitiveQueryKey(key) {
			values.Set(key, "[redacted]")
		}
	}
	return values.Encode()
}

func summarizeQueryForLog(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "[redacted]"
	}
	keep := []string{"c", "v", "f", "id", "query", "size", "offset", "submission", "time"}
	out := make([]string, 0, len(keep))
	for _, k := range keep {
		v := strings.TrimSpace(values.Get(k))
		if v == "" {
			continue
		}
		if k == "query" && len(v) > 40 {
			v = v[:40] + "..."
		}
		if k == "id" && len(v) > 24 {
			v = v[:24] + "..."
		}
		out = append(out, fmt.Sprintf("%s=%s", k, strconv.Quote(v)))
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, " ")
}

func isSensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "u", "p", "t", "s", "token", "password", "api_key", "apikey":
		return true
	default:
		return false
	}
}

func (s *Server) withTimeout(r *http.Request, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = 20
	}
	return context.WithTimeout(r.Context(), time.Duration(sec)*time.Second)
}

func onePixelPNG() []byte {
	return []byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 73, 72, 68, 82, 0, 0, 0, 1, 0, 0, 0, 1, 8, 4, 0, 0, 0, 181, 28, 12, 2, 0, 0, 0, 11, 73, 68, 65, 84, 120, 218, 99, 252, 255, 31, 0, 3, 3, 2, 0, 239, 166, 133, 39, 0, 0, 0, 0, 73, 69, 78, 68, 174, 66, 96, 130}
}
