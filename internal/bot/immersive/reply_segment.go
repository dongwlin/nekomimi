package immersive

import (
	"math/rand"
	"strings"
	"time"
)

const ReplySegmentDelimiter = "\n---\n"

const (
	shortSegmentRuneThreshold = 6
	shortSegmentDelayMinMS    = 300
	shortSegmentDelayMaxMS    = 500
	longSegmentDelayMinMS     = 400
	longSegmentDelayMaxMS     = 800
)

// ReplySegmentAccumulator incrementally parses assistant output by a strict
// delimiter protocol ("\n---\n") and yields completed non-empty segments.
type ReplySegmentAccumulator struct {
	buffer strings.Builder
}

func NewReplySegmentAccumulator() *ReplySegmentAccumulator {
	return &ReplySegmentAccumulator{}
}

func (a *ReplySegmentAccumulator) Append(delta string) []string {
	if a == nil || delta == "" {
		return nil
	}
	a.buffer.WriteString(delta)
	return a.drain(false)
}

func (a *ReplySegmentAccumulator) FlushTail() []string {
	if a == nil {
		return nil
	}
	return a.drain(true)
}

func (a *ReplySegmentAccumulator) drain(flushTail bool) []string {
	source := a.buffer.String()
	if source == "" {
		return nil
	}

	segments := make([]string, 0, 4)
	start := 0
	for {
		idx := strings.Index(source[start:], ReplySegmentDelimiter)
		if idx < 0 {
			break
		}
		cut := start + idx
		if piece := strings.TrimSpace(source[start:cut]); piece != "" {
			segments = append(segments, piece)
		}
		start = cut + len(ReplySegmentDelimiter)
	}

	rest := source[start:]
	a.buffer.Reset()
	if flushTail {
		if piece := strings.TrimSpace(rest); piece != "" {
			segments = append(segments, piece)
		}
		return segments
	}

	a.buffer.WriteString(rest)
	return segments
}

func SplitReplySegments(text string) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	parts := strings.Split(text, ReplySegmentDelimiter)
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if piece := strings.TrimSpace(part); piece != "" {
			segments = append(segments, piece)
		}
	}
	return segments
}

func NextReplySegmentDelay(segment string) time.Duration {
	minMS := shortSegmentDelayMinMS
	maxMS := shortSegmentDelayMaxMS
	if len([]rune(strings.TrimSpace(segment))) > shortSegmentRuneThreshold {
		minMS = longSegmentDelayMinMS
		maxMS = longSegmentDelayMaxMS
	}
	if maxMS <= minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+rand.Intn(maxMS-minMS+1)) * time.Millisecond
}
