package immersive

import (
	"math/rand"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

type streamChunkAccumulator struct {
	minChunkChars int
	maxChunkChars int
	buffer        strings.Builder
}

func newStreamChunkAccumulator(cfg config.ContinuousSpeechConfig) *streamChunkAccumulator {
	return &streamChunkAccumulator{
		minChunkChars: cfg.MinChunkChars,
		maxChunkChars: cfg.MaxChunkChars,
	}
}

func (a *streamChunkAccumulator) Append(delta string) []string {
	if strings.TrimSpace(delta) == "" {
		return nil
	}
	a.buffer.WriteString(delta)
	chunks, rest := splitReplyChunks(a.buffer.String(), a.minChunkChars, a.maxChunkChars, false)
	a.buffer.Reset()
	a.buffer.WriteString(rest)
	return chunks
}

func (a *streamChunkAccumulator) FlushTail() []string {
	chunks, rest := splitReplyChunks(a.buffer.String(), a.minChunkChars, a.maxChunkChars, true)
	a.buffer.Reset()
	a.buffer.WriteString(rest)
	if strings.TrimSpace(a.buffer.String()) == "" {
		a.buffer.Reset()
	}
	return chunks
}

func splitReplyChunks(text string, minChars, maxChars int, flushTail bool) ([]string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil, ""
	}
	if minChars <= 0 {
		minChars = 12
	}
	if maxChars <= 0 {
		maxChars = 80
	}
	if maxChars < minChars {
		maxChars = minChars
	}
	source := trimmed
	chunks := make([]string, 0, 4)
	startByte := 0
	startRune := 0
	lastBoundaryByte := -1
	runeCount := 0
	for idx, r := range source {
		runeCount++
		if isChunkBoundary(r) {
			lastBoundaryByte = idx + len(string(r))
		}
		segmentRunes := runeCount - startRune
		if segmentRunes < minChars && segmentRunes < maxChars {
			continue
		}
		cutByte := -1
		if lastBoundaryByte > startByte && segmentRunes >= minChars {
			cutByte = lastBoundaryByte
		}
		if segmentRunes >= maxChars {
			if cutByte < 0 {
				cutByte = idx + len(string(r))
			}
		}
		if cutByte < 0 {
			continue
		}
		chunk := strings.TrimSpace(source[startByte:cutByte])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		startByte = cutByte
		startRune = runeCount
		lastBoundaryByte = -1
	}
	rest := strings.TrimSpace(source[startByte:])
	if flushTail && rest != "" {
		chunks = append(chunks, rest)
		return chunks, ""
	}
	return chunks, rest
}

func isChunkBoundary(r rune) bool {
	switch r {
	case '。', '！', '？', '!', '?', '.', ';', '；', '\n':
		return true
	default:
		return false
	}
}

func nextContinuousSpeechDelay(cfg config.ContinuousSpeechConfig) time.Duration {
	minMS := cfg.MinIntervalMS
	maxMS := cfg.MaxIntervalMS
	if minMS <= 0 {
		minMS = 300
	}
	if maxMS <= 0 {
		maxMS = 900
	}
	if maxMS < minMS {
		maxMS = minMS
	}
	if maxMS == minMS {
		return time.Duration(minMS) * time.Millisecond
	}
	return time.Duration(minMS+rand.Intn(maxMS-minMS+1)) * time.Millisecond
}
