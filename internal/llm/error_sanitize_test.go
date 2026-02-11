package llm

import (
	"errors"
	"testing"
)

func TestUserVisibleError_RedactsURL(t *testing.T) {
	err := errors.New(`Post "https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions": context deadline exceeded`)
	got := UserVisibleError(err)
	want := `Post "[redacted-url]": context deadline exceeded`
	if got != want {
		t.Fatalf("unexpected sanitized error: got %q, want %q", got, want)
	}
}

func TestUserVisibleError_Empty(t *testing.T) {
	err := errors.New("   ")
	got := UserVisibleError(err)
	if got != "未知错误" {
		t.Fatalf("unexpected fallback message: %q", got)
	}
}

