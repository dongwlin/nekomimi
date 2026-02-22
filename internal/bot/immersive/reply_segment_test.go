package immersive

import "testing"

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
