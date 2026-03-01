package provider

import (
	"context"
	"testing"
)

func TestPreferredQualityFromContext(t *testing.T) {
	ctx := WithPreferredQuality(context.Background(), "  HI_RES  ")
	if got := PreferredQuality(ctx); got != "HI_RES" {
		t.Fatalf("expected HI_RES, got %q", got)
	}
}

func TestPreferredQualityEmpty(t *testing.T) {
	ctx := WithPreferredQuality(context.Background(), "   ")
	if got := PreferredQuality(ctx); got != "" {
		t.Fatalf("expected empty quality, got %q", got)
	}
}

func TestStrictQualityContext(t *testing.T) {
	ctx := WithStrictQuality(context.Background(), true)
	if !StrictQuality(ctx) {
		t.Fatalf("expected strict quality true")
	}
}
