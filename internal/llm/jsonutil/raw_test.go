package jsonutil

import (
	"encoding/json"
	"testing"
)

func TestNormalizeObjectArguments(t *testing.T) {
	tests := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{name: "nil", in: nil, want: "{}"},
		{name: "empty", in: json.RawMessage(""), want: "{}"},
		{name: "whitespace", in: json.RawMessage(" \n\t "), want: "{}"},
		{name: "object", in: json.RawMessage(`{"a":1}`), want: `{"a":1}`},
		{name: "array", in: json.RawMessage(`[1,2]`), want: `[1,2]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeObjectArguments(tt.in)
			if string(got) != tt.want {
				t.Fatalf("normalize mismatch: got %q, want %q", string(got), tt.want)
			}
		})
	}
}

func TestCloneRawMessage_DetachedCopy(t *testing.T) {
	src := json.RawMessage(`{"x":1}`)
	cloned := CloneRawMessage(src)
	if string(cloned) != string(src) {
		t.Fatalf("clone content mismatch: got %q, want %q", string(cloned), string(src))
	}

	src[0] = '['
	if string(cloned) != `{"x":1}` {
		t.Fatalf("clone should be detached copy, got %q", string(cloned))
	}
}

func TestCloneRawMessage_Empty(t *testing.T) {
	if got := CloneRawMessage(nil); got != nil {
		t.Fatalf("clone nil mismatch: got %q, want nil", string(got))
	}
}

func TestCompactRawMessage(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := CompactRawMessage(nil)
		if string(got) != "{}" {
			t.Fatalf("empty compact mismatch: got %q, want %q", string(got), "{}")
		}
	})

	t.Run("valid json compacted", func(t *testing.T) {
		got := CompactRawMessage(json.RawMessage("{\n  \"a\": 1\n}"))
		if string(got) != `{"a":1}` {
			t.Fatalf("compact mismatch: got %q, want %q", string(got), `{"a":1}`)
		}
	})

	t.Run("invalid json fallback clone", func(t *testing.T) {
		src := json.RawMessage(`{"a":1`)
		got := CompactRawMessage(src)
		if string(got) != string(src) {
			t.Fatalf("invalid fallback mismatch: got %q, want %q", string(got), string(src))
		}
		src[0] = '['
		if string(got) != `{"a":1` {
			t.Fatalf("invalid fallback should be detached copy, got %q", string(got))
		}
	})
}
