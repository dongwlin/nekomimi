package immersive

import (
	"strings"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestSplitReplyChunks_ByPunctuation(t *testing.T) {
	text := "第一句。第二句！第三句？"
	chunks, rest := splitReplyChunks(text, 4, 30, false)
	if rest != "" {
		t.Fatalf("expected empty rest, got %q", rest)
	}
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d (%v)", len(chunks), chunks)
	}
}

func TestSplitReplyChunks_MaxCharsFallback(t *testing.T) {
	text := strings.Repeat("啊", 60)
	chunks, rest := splitReplyChunks(text, 12, 20, false)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks by max chars, got %d", len(chunks))
	}
	if len([]rune(rest)) != 0 {
		t.Fatalf("expected empty rest, got %q", rest)
	}
}

func TestStreamChunkAccumulator_FlushTail(t *testing.T) {
	acc := newStreamChunkAccumulator(config.ContinuousSpeechConfig{
		MinChunkChars: 8,
		MaxChunkChars: 20,
	})
	chunks := acc.Append("这是一段没有标点")
	if len(chunks) != 0 {
		t.Fatalf("expected no chunk before flush, got %v", chunks)
	}
	tail := acc.FlushTail()
	if len(tail) != 1 {
		t.Fatalf("expected one tail chunk, got %d", len(tail))
	}
}
