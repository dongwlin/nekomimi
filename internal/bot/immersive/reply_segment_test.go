package immersive

import (
	"testing"

	"github.com/dongwlin/nekomimi/internal/ctxasm"
)

func TestSplitReplySegments_StrictDelimiter(t *testing.T) {
	segments := SplitReplySegments("first\n---\nsecond")
	if len(segments) != 2 || segments[0] != "first" || segments[1] != "second" {
		t.Fatalf("unexpected segments: %#v", segments)
	}

	segments = SplitReplySegments("first\r\n---\r\nsecond")
	if len(segments) != 1 || segments[0] != "first\r\n---\r\nsecond" {
		t.Fatalf("expected no split for CRLF variant, got %#v", segments)
	}
}

func TestReplySegmentAccumulator_CrossDeltaDelimiter(t *testing.T) {
	acc := NewReplySegmentAccumulator()
	if out := acc.Append("hello\n--"); len(out) != 0 {
		t.Fatalf("expected no segment before delimiter completion, got %#v", out)
	}

	out := acc.Append("-\nworld\n---\nnext")
	if len(out) != 2 || out[0] != "hello" || out[1] != "world" {
		t.Fatalf("unexpected streamed segments: %#v", out)
	}

	tail := acc.FlushTail()
	if len(tail) != 1 || tail[0] != "next" {
		t.Fatalf("unexpected tail segments: %#v", tail)
	}
}

func TestSplitReplySegments_IgnoreEmptySegments(t *testing.T) {
	segments := SplitReplySegments("\n---\nA\n---\n\n---\nB\n---\n")
	if len(segments) != 2 || segments[0] != "A" || segments[1] != "B" {
		t.Fatalf("unexpected segments: %#v", segments)
	}
}

func TestReplySegmentAccumulator_FlushTailWithoutDelimiter(t *testing.T) {
	acc := NewReplySegmentAccumulator()
	if out := acc.Append("single message"); len(out) != 0 {
		t.Fatalf("expected no segment before flush, got %#v", out)
	}
	tail := acc.FlushTail()
	if len(tail) != 1 || tail[0] != "single message" {
		t.Fatalf("unexpected tail: %#v", tail)
	}
}

func TestSplitReplySegmentsForDelivery_FallsBackToSentenceBoundaries(t *testing.T) {
	segments := SplitReplySegmentsForDelivery("先打个招呼。再补一句！最后收尾喵~", 3)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %#v", segments)
	}
	if segments[0] != "先打个招呼。" || segments[1] != "再补一句！" || segments[2] != "最后收尾喵~" {
		t.Fatalf("unexpected sentence split: %#v", segments)
	}
}

func TestSplitReplySegmentsForDelivery_GreetingFallback(t *testing.T) {
	segments := SplitReplySegmentsForDelivery("晚上好刚摸完鱼啃了便利店的冰皮月亮还不错喵~", 2)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %#v", segments)
	}
	if segments[0] != "晚上好" {
		t.Fatalf("unexpected greeting segment: %#v", segments)
	}
	if segments[1] != "刚摸完鱼啃了便利店的冰皮月亮还不错喵~" {
		t.Fatalf("unexpected tail segment: %#v", segments)
	}
}

func TestSplitReplySegmentsForDelivery_RespectsMaxSegments(t *testing.T) {
	segments := SplitReplySegmentsForDelivery("一。二。三。", 2)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %#v", segments)
	}
	if segments[0] != "一。" || segments[1] != "二。\n三。" {
		t.Fatalf("unexpected merged segments: %#v", segments)
	}
}

func TestReplySegmentLimit_UsesImmersiveContext(t *testing.T) {
	if got := replySegmentLimit(nil); got != 1 {
		t.Fatalf("expected default segment limit 1, got %d", got)
	}
	if got := replySegmentLimit(&ctxasm.ImmersiveContext{MaxReplySegments: 3}); got != 3 {
		t.Fatalf("expected immersive segment limit 3, got %d", got)
	}
}

func TestNextReplySegmentDelay_BySegmentLength(t *testing.T) {
	for i := 0; i < 20; i++ {
		shortDelayMS := NextReplySegmentDelay("short").Milliseconds()
		if shortDelayMS < 300 || shortDelayMS > 500 {
			t.Fatalf("short delay out of range: %d", shortDelayMS)
		}

		longDelayMS := NextReplySegmentDelay("this-is-longer-than-six")
		if ms := longDelayMS.Milliseconds(); ms < 400 || ms > 800 {
			t.Fatalf("long delay out of range: %d", ms)
		}
	}
}
