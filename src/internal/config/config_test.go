package config

import "testing"

func TestLoadAppliesEnvOverrides(t *testing.T) {
	t.Setenv("DHWANI_PORT", "9091")
	t.Setenv("DHWANI_HOST", "127.0.0.1")
	t.Setenv("DHWANI_USERNAME", "alice")
	t.Setenv("DHWANI_PASSWORD", "secret")
	t.Setenv("DHWANI_DB_PATH", "/tmp/dhwani-test.db")
	t.Setenv("DHWANI_STREAM_QUALITY", "HIGH")
	t.Setenv("DHWANI_CLIENT_HEADER", "MyClient/1.0")
	t.Setenv("DHWANI_INGEST_ON_STREAM", "true")
	t.Setenv("DHWANI_INGEST_ON_STAR", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Server.Port != 9091 {
		t.Fatalf("expected port 9091, got %d", cfg.Server.Port)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("expected host 127.0.0.1, got %q", cfg.Server.Host)
	}
	if cfg.Auth.Username != "alice" || cfg.Auth.Password != "secret" {
		t.Fatalf("unexpected auth: %#v", cfg.Auth)
	}
	if cfg.Database.Path != "/tmp/dhwani-test.db" {
		t.Fatalf("unexpected db path: %q", cfg.Database.Path)
	}
	if cfg.Upstream.StreamQuality != "HIGH" {
		t.Fatalf("unexpected quality: %q", cfg.Upstream.StreamQuality)
	}
	if cfg.Upstream.ClientHeader != "MyClient/1.0" {
		t.Fatalf("unexpected client header: %q", cfg.Upstream.ClientHeader)
	}
	if !cfg.Server.IngestOnStream {
		t.Fatalf("expected ingest_on_stream=true")
	}
	if cfg.Server.IngestOnStar {
		t.Fatalf("expected ingest_on_star=false")
	}
	if cfg.Address() != "127.0.0.1:9091" {
		t.Fatalf("unexpected address: %q", cfg.Address())
	}
}

func TestValidateRequiresUpstreamSource(t *testing.T) {
	cfg := Default()
	cfg.Upstream.InstancesURL = ""
	cfg.Upstream.InstancesFile = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected Validate() to fail without upstream source")
	}
}

func TestValidateRequiresAuthAndPort(t *testing.T) {
	cfg := Default()
	cfg.Auth.Username = ""
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected Validate() to fail without username")
	}

	cfg = Default()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected Validate() to fail with invalid port")
	}
}
