package provider

import (
	"context"
	"strings"
)

type qualityContextKey struct{}
type strictQualityContextKey struct{}

func WithPreferredQuality(ctx context.Context, quality string) context.Context {
	quality = strings.TrimSpace(quality)
	if quality == "" {
		return ctx
	}
	return context.WithValue(ctx, qualityContextKey{}, quality)
}

func PreferredQuality(ctx context.Context) string {
	v, _ := ctx.Value(qualityContextKey{}).(string)
	return strings.TrimSpace(v)
}

func WithStrictQuality(ctx context.Context, strict bool) context.Context {
	return context.WithValue(ctx, strictQualityContextKey{}, strict)
}

func StrictQuality(ctx context.Context) bool {
	v, _ := ctx.Value(strictQualityContextKey{}).(bool)
	return v
}
