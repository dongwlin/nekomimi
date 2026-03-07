package immersive

import (
	"math/rand"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
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

func SplitReplySegmentsForDelivery(text string, maxSegments int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}

	segments := SplitReplySegments(trimmed)
	if len(segments) > 1 {
		return clampReplySegments(segments, maxSegments)
	}
	if maxSegments == 1 {
		return []string{trimmed}
	}

	if segments = splitReplySegmentsByLines(trimmed); len(segments) > 1 {
		return clampReplySegments(segments, maxSegments)
	}
	if segments = splitReplySegmentsBySentence(trimmed); len(segments) > 1 {
		return clampReplySegments(segments, maxSegments)
	}
	if segments = splitReplySegmentsByGreeting(trimmed); len(segments) > 1 {
		return clampReplySegments(segments, maxSegments)
	}
	return []string{trimmed}
}

func replySegmentLimit(immersiveCtx *contextassemble.ImmersiveContext) int {
	if immersiveCtx == nil || immersiveCtx.MaxReplySegments <= 0 {
		return 1
	}
	return immersiveCtx.MaxReplySegments
}

func clampReplySegments(segments []string, maxSegments int) []string {
	if len(segments) == 0 {
		return nil
	}
	if maxSegments <= 0 || len(segments) <= maxSegments {
		return append([]string(nil), segments...)
	}
	if maxSegments == 1 {
		return []string{strings.TrimSpace(strings.Join(segments, "\n"))}
	}

	merged := make([]string, 0, maxSegments)
	merged = append(merged, segments[:maxSegments-1]...)
	tail := strings.TrimSpace(strings.Join(segments[maxSegments-1:], "\n"))
	if tail != "" {
		merged = append(merged, tail)
	}
	return merged
}

func splitReplySegmentsByLines(text string) []string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.Contains(normalized, "\n") {
		return nil
	}
	parts := strings.Split(normalized, "\n")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if piece := strings.TrimSpace(part); piece != "" {
			segments = append(segments, piece)
		}
	}
	if len(segments) <= 1 {
		return nil
	}
	return segments
}

func splitReplySegmentsBySentence(text string) []string {
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}

	segments := make([]string, 0, 4)
	start := 0
	for idx, ch := range runes {
		if !isSentenceBoundaryRune(ch) {
			continue
		}
		piece := strings.TrimSpace(string(runes[start : idx+1]))
		if piece != "" {
			segments = append(segments, piece)
		}
		start = idx + 1
	}
	if tail := strings.TrimSpace(string(runes[start:])); tail != "" {
		segments = append(segments, tail)
	}
	if len(segments) <= 1 {
		return nil
	}
	return segments
}

func isSentenceBoundaryRune(ch rune) bool {
	switch ch {
	case '。', '！', '？', '!', '?', '~', '～', '…':
		return true
	default:
		return false
	}
}

func splitReplySegmentsByGreeting(text string) []string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) < 10 {
		return nil
	}

	for _, greeting := range []string{"早上好", "上午好", "中午好", "下午好", "晚上好", "你好", "哈喽", "嗨", "嘿"} {
		if !strings.HasPrefix(string(runes), greeting) {
			continue
		}
		cut := len([]rune(greeting))
		for cut < len(runes) {
			switch runes[cut] {
			case '呀', '啊', '啦', '喔', '喵', '~', '～', '，', ',', '！', '!', '？', '?', ' ':
				cut++
			default:
				goto done
			}
		}
	done:
		first := strings.TrimSpace(string(runes[:cut]))
		rest := strings.TrimSpace(string(runes[cut:]))
		if first != "" && rest != "" && len([]rune(rest)) >= 6 {
			return []string{first, rest}
		}
	}
	return nil
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
