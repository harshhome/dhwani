package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"dhwani/internal/model"
)

const (
	maxDashSegments = 10000
	dashCacheTTL    = 10 * time.Minute
)

var errInvalidRange = errors.New("invalid range")

type dashManifest struct {
	Period struct {
		AdaptationSet struct {
			MIMEType       string `xml:"mimeType,attr"`
			Representation struct {
				ID              string `xml:"id,attr"`
				SegmentTemplate struct {
					Initialization  string `xml:"initialization,attr"`
					Media           string `xml:"media,attr"`
					StartNumber     int    `xml:"startNumber,attr"`
					SegmentTimeline struct {
						Segments []struct {
							D int64 `xml:"d,attr"`
							R int   `xml:"r,attr"`
						} `xml:"S"`
					} `xml:"SegmentTimeline"`
				} `xml:"SegmentTemplate"`
			} `xml:"Representation"`
		} `xml:"AdaptationSet"`
	} `xml:"Period"`
}

type dashPlan struct {
	ContentType string
	InitURL     string
	SegmentURLs []string
}

type dashChunk struct {
	URL    string
	Start  int64
	End    int64
	Length int64
}

type dashByteMap struct {
	Total       int64
	Chunks      []dashChunk
	ContentType string
	ExpiresAt   time.Time
}

func parseDashPlan(manifestB64 string) (dashPlan, error) {
	b, err := base64.StdEncoding.DecodeString(manifestB64)
	if err != nil {
		return dashPlan{}, fmt.Errorf("base64 decode: %w", err)
	}
	var mpd dashManifest
	if err := xml.Unmarshal(b, &mpd); err != nil {
		return dashPlan{}, fmt.Errorf("xml decode: %w", err)
	}
	tpl := mpd.Period.AdaptationSet.Representation.SegmentTemplate
	if strings.TrimSpace(tpl.Initialization) == "" || strings.TrimSpace(tpl.Media) == "" {
		return dashPlan{}, fmt.Errorf("segment template missing initialization/media")
	}
	startNum := tpl.StartNumber
	if startNum <= 0 {
		startNum = 1
	}
	reprID := mpd.Period.AdaptationSet.Representation.ID
	segURLs := make([]string, 0, 256)
	segNum := startNum
	for _, s := range tpl.SegmentTimeline.Segments {
		if s.D <= 0 {
			return dashPlan{}, fmt.Errorf("invalid segment duration")
		}
		if s.R < 0 {
			return dashPlan{}, fmt.Errorf("negative segment repeat not supported")
		}
		repeat := s.R + 1
		for i := 0; i < repeat; i++ {
			u := strings.ReplaceAll(tpl.Media, "$Number$", strconv.Itoa(segNum))
			u = strings.ReplaceAll(u, "$RepresentationID$", reprID)
			segURLs = append(segURLs, u)
			segNum++
			if len(segURLs) > maxDashSegments {
				return dashPlan{}, fmt.Errorf("too many segments")
			}
		}
	}
	if len(segURLs) == 0 {
		return dashPlan{}, fmt.Errorf("no media segments")
	}

	initURL := strings.ReplaceAll(tpl.Initialization, "$RepresentationID$", reprID)
	contentType := strings.TrimSpace(mpd.Period.AdaptationSet.MIMEType)
	if contentType == "" {
		contentType = "audio/mp4"
	}
	return dashPlan{
		ContentType: contentType,
		InitURL:     initURL,
		SegmentURLs: segURLs,
	}, nil
}

func (s *Server) streamDASH(w http.ResponseWriter, r *http.Request, id string, res model.StreamResolution) error {
	plan, err := parseDashPlan(res.ManifestBase64)
	if err != nil {
		return fmt.Errorf("parse dash manifest: %w", err)
	}
	byteMap, err := s.getOrBuildDashMap(r.Context(), res, plan)
	if err != nil {
		return fmt.Errorf("build dash byte map: %w", err)
	}

	start, end, hasRange, err := parseStreamRange(r.Header.Get("Range"), byteMap.Total)
	if err != nil {
		return fmt.Errorf("%w: %v", errInvalidRange, err)
	}

	w.Header().Set("Accept-Ranges", "bytes")
	if byteMap.ContentType != "" {
		w.Header().Set("Content-Type", byteMap.ContentType)
	}
	if !hasRange {
		w.Header().Set("Content-Length", strconv.FormatInt(byteMap.Total, 10))
		w.WriteHeader(http.StatusOK)
		if err := s.copyDashBytes(r.Context(), w, byteMap, 0, byteMap.Total-1); err != nil && r.Context().Err() == nil {
			s.logger.Warn("dash stream copy error", "id", id, "err", err)
		}
		return nil
	}

	length := end - start + 1
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, byteMap.Total))
	w.WriteHeader(http.StatusPartialContent)
	if err := s.copyDashBytes(r.Context(), w, byteMap, start, end); err != nil && r.Context().Err() == nil {
		s.logger.Warn("dash stream copy error", "id", id, "err", err)
	}
	return nil
}

func (s *Server) getOrBuildDashMap(ctx context.Context, res model.StreamResolution, plan dashPlan) (dashByteMap, error) {
	key := s.dashCacheKey(res)
	now := time.Now()
	s.dashCacheMu.RLock()
	if v, ok := s.dashCache[key]; ok && now.Before(v.ExpiresAt) {
		s.dashCacheMu.RUnlock()
		return v, nil
	}
	s.dashCacheMu.RUnlock()

	urls := make([]string, 0, len(plan.SegmentURLs)+1)
	urls = append(urls, plan.InitURL)
	urls = append(urls, plan.SegmentURLs...)

	chunks := make([]dashChunk, 0, len(urls))
	var pos int64
	for _, u := range urls {
		n, err := s.probeContentLength(ctx, u)
		if err != nil {
			return dashByteMap{}, err
		}
		if n <= 0 {
			return dashByteMap{}, fmt.Errorf("invalid content length for segment")
		}
		chunks = append(chunks, dashChunk{
			URL:    u,
			Start:  pos,
			End:    pos + n - 1,
			Length: n,
		})
		pos += n
	}

	out := dashByteMap{
		Total:       pos,
		Chunks:      chunks,
		ContentType: plan.ContentType,
		ExpiresAt:   now.Add(dashCacheTTL),
	}
	s.dashCacheMu.Lock()
	s.dashCache[key] = out
	s.dashCacheMu.Unlock()
	return out, nil
}

func (s *Server) dashCacheKey(res model.StreamResolution) string {
	hash := strings.TrimSpace(res.ManifestHash)
	if hash == "" {
		hash = strings.TrimSpace(res.ManifestBase64)
		if len(hash) > 64 {
			hash = hash[:64]
		}
	}
	return res.Provider + ":" + res.TrackProviderID + ":" + hash
}

func (s *Server) probeContentLength(ctx context.Context, target string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return 0, err
	}
	resp, err := s.httpClient.Do(req)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode < 400 {
			if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && n > 0 {
				return n, nil
			}
		}
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err = s.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("segment probe status %d", resp.StatusCode)
	}
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		total, err := parseContentRangeTotal(cr)
		if err == nil && total > 0 {
			return total, nil
		}
	}
	if n, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); err == nil && n > 0 {
		if resp.StatusCode == http.StatusPartialContent {
			return n, nil
		}
		return n, nil
	}
	return 0, fmt.Errorf("unable to probe segment length")
}

func parseContentRangeTotal(v string) (int64, error) {
	// Format: bytes 0-0/12345
	parts := strings.Split(strings.TrimSpace(v), "/")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid content-range")
	}
	if parts[1] == "*" {
		return 0, fmt.Errorf("unknown content-range total")
	}
	return strconv.ParseInt(parts[1], 10, 64)
}

func parseStreamRange(rangeHeader string, total int64) (int64, int64, bool, error) {
	if strings.TrimSpace(rangeHeader) == "" {
		return 0, 0, false, nil
	}
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, false, fmt.Errorf("unsupported range unit")
	}
	raw := strings.TrimPrefix(rangeHeader, "bytes=")
	if strings.Contains(raw, ",") {
		return 0, 0, false, fmt.Errorf("multiple ranges not supported")
	}
	pair := strings.SplitN(raw, "-", 2)
	if len(pair) != 2 {
		return 0, 0, false, fmt.Errorf("invalid range format")
	}
	if total <= 0 {
		return 0, 0, false, fmt.Errorf("invalid total length")
	}
	startRaw := strings.TrimSpace(pair[0])
	endRaw := strings.TrimSpace(pair[1])

	var start, end int64
	switch {
	case startRaw == "":
		// suffix range: bytes=-500
		n, err := strconv.ParseInt(endRaw, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false, fmt.Errorf("invalid suffix range")
		}
		if n > total {
			n = total
		}
		start = total - n
		end = total - 1
	default:
		v, err := strconv.ParseInt(startRaw, 10, 64)
		if err != nil || v < 0 {
			return 0, 0, false, fmt.Errorf("invalid range start")
		}
		start = v
		if endRaw == "" {
			end = total - 1
		} else {
			v, err := strconv.ParseInt(endRaw, 10, 64)
			if err != nil || v < start {
				return 0, 0, false, fmt.Errorf("invalid range end")
			}
			end = v
		}
	}
	if start >= total {
		return 0, 0, false, fmt.Errorf("range start out of bounds")
	}
	if end >= total {
		end = total - 1
	}
	return start, end, true, nil
}

func (s *Server) copyDashBytes(ctx context.Context, w io.Writer, m dashByteMap, from, to int64) error {
	for _, chunk := range m.Chunks {
		if to < chunk.Start || from > chunk.End {
			continue
		}
		localStart := int64(0)
		if from > chunk.Start {
			localStart = from - chunk.Start
		}
		localEnd := chunk.Length - 1
		if to < chunk.End {
			localEnd = to - chunk.Start
		}
		if err := s.copyChunkRange(ctx, w, chunk.URL, localStart, localEnd); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) copyChunkRange(ctx context.Context, w io.Writer, target string, start, end int64) error {
	if start < 0 || end < start {
		return fmt.Errorf("invalid chunk range")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	full := start == 0
	if !full || end >= 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("chunk status %d", resp.StatusCode)
	}

	want := end - start + 1
	if resp.StatusCode == http.StatusOK && start > 0 {
		if _, err := io.CopyN(io.Discard, resp.Body, start); err != nil {
			return err
		}
	}
	_, err = io.CopyN(w, resp.Body, want)
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}
