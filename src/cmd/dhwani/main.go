package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"dhwani/internal/auth"
	"dhwani/internal/config"
	"dhwani/internal/db"
	"dhwani/internal/httpapi"
	"dhwani/internal/provider"
	"dhwani/internal/provider/squid"
	"dhwani/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(cfg.Logging.Level)}))

	store, err := db.Open(cfg.Database.Path)
	if err != nil {
		logger.Error("failed to init sqlite", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	registry := provider.NewRegistry()
	providerTimeout := timeoutForProvider(cfg.Server.DefaultTimeoutSec, cfg.Upstream.TimeoutSec)
	instancesClient := httpClientWithUserAgent(providerTimeout, cfg.Server.UserAgent)
	instances := make([]string, 0, 16)
	if strings.TrimSpace(cfg.Upstream.InstancesURL) != "" {
		remote, ferr := fetchMonochromeInstances(instancesClient, cfg.Upstream.InstancesURL)
		if ferr != nil {
			logger.Error("failed to fetch upstream instances", "url", cfg.Upstream.InstancesURL, "err", ferr)
			os.Exit(1)
		}
		instances = mergeInstances(instances, remote)
	}
	if strings.TrimSpace(cfg.Upstream.InstancesFile) != "" {
		local, ferr := fetchMonochromeInstances(nil, cfg.Upstream.InstancesFile)
		if ferr != nil {
			logger.Error("failed to load local instances file", "path", cfg.Upstream.InstancesFile, "err", ferr)
			os.Exit(1)
		}
		instances = mergeInstances(instances, local)
	}
	for i, inst := range instances {
		name := fmt.Sprintf("mx%d", i+1)
		client := httpClientWithUserAgent(providerTimeout, cfg.Server.UserAgent)
		sp, err := squid.New(squid.Config{
			Name:          name,
			BaseURL:       inst,
			ClientHeader:  cfg.Upstream.ClientHeader,
			Source:        cfg.Upstream.Source,
			StreamQuality: cfg.Upstream.StreamQuality,
		}, client)
		if err != nil {
			logger.Warn("failed to create provider", "name", name, "base_url", inst, "err", err)
			continue
		}
		if err := registry.Register(sp); err != nil {
			logger.Warn("failed to register provider", "name", name, "base_url", inst, "err", err)
			continue
		}
		logger.Info("registered provider instance", "name", name, "base_url", inst)
	}
	if len(registry.Enabled()) == 0 {
		logger.Error("no valid upstream providers registered")
		os.Exit(1)
	}

	catalog := service.NewCatalogService(registry, store, logger)
	attemptTimeout := cfg.Upstream.AttemptTimeoutSec
	if attemptTimeout <= 0 {
		attemptTimeout = 6
	}
	catalog.SetProviderAttemptTimeout(time.Duration(attemptTimeout) * time.Second)
	fallbackAttempts := cfg.Upstream.FallbackAttempts
	if fallbackAttempts <= 0 {
		fallbackAttempts = 2
	}
	catalog.SetMaxProviderAttempts(fallbackAttempts)
	probeEvery := cfg.Upstream.ProbeIntervalSec
	if probeEvery <= 0 {
		probeEvery = 10800
	}
	catalog.StartLatencyProber(context.Background(), time.Duration(probeEvery)*time.Second)

	streamClient := httpClientWithUserAgent(0, cfg.Server.UserAgent)
	srv := httpapi.NewServer(
		logger,
		catalog,
		auth.Credentials{Username: cfg.Auth.Username, Password: cfg.Auth.Password},
		streamClient,
		cfg.Server.EnableJSONResponses,
		cfg.Server.IngestOnStream,
		cfg.Server.IngestOnStar,
	)

	httpServer := &http.Server{
		Addr:              cfg.Address(),
		Handler:           srv.Router(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("dhwani listening", "addr", cfg.Address())
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sig:
	case err := <-serverErr:
		if err != nil {
			logger.Error("server failed", "err", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	logger.Info("server stopped")
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func timeoutForProvider(defaultSec int, providerSec int) time.Duration {
	t := defaultSec
	if providerSec > 0 {
		t = providerSec
	}
	if t <= 0 {
		t = 20
	}
	return time.Duration(t) * time.Second
}

type instancesPayload struct {
	API []string `json:"api"`
}

func fetchMonochromeInstances(client *http.Client, instancesURL string) ([]string, error) {
	src := strings.TrimSpace(instancesURL)
	if src == "" {
		return nil, fmt.Errorf("instances source is empty")
	}
	var (
		b   []byte
		err error
	)
	if isHTTPSource(src) {
		if client == nil {
			client = httpClientWithUserAgent(20*time.Second, "")
		}
		req, err := http.NewRequest(http.MethodGet, src, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("instances endpoint status %d", resp.StatusCode)
		}
		b, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
	} else {
		path := localInstancesPath(src)
		b, err = os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read instances file %q: %w", path, err)
		}
	}
	var p instancesPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return mergeInstances(p.API), nil
}

func mergeInstances(lists ...[]string) []string {
	uniq := map[string]struct{}{}
	out := make([]string, 0, 16)
	for _, list := range lists {
		for _, raw := range list {
			base := normalizeBaseURL(raw)
			if base == "" {
				continue
			}
			if _, ok := uniq[base]; ok {
				continue
			}
			uniq[base] = struct{}{}
			out = append(out, base)
		}
	}
	sort.Strings(out)
	return out
}

func isHTTPSource(src string) bool {
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

func localInstancesPath(src string) string {
	if strings.HasPrefix(strings.ToLower(src), "file://") {
		if u, err := url.Parse(src); err == nil && u.Path != "" {
			return u.Path
		}
	}
	return filepath.Clean(src)
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.TrimRight(u.String(), "/")
}

type userAgentRoundTripper struct {
	base http.RoundTripper
	ua   string
}

func (rt userAgentRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if strings.TrimSpace(rt.ua) != "" && strings.TrimSpace(req.Header.Get("User-Agent")) == "" {
		req = req.Clone(req.Context())
		req.Header.Set("User-Agent", rt.ua)
	}
	return rt.base.RoundTrip(req)
}

func httpClientWithUserAgent(timeout time.Duration, ua string) *http.Client {
	base := http.DefaultTransport
	return &http.Client{
		Timeout: timeout,
		Transport: userAgentRoundTripper{
			base: base,
			ua:   ua,
		},
	}
}
