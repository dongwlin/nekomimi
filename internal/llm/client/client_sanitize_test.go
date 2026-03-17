package client

import "testing"

func TestRedactURLs(t *testing.T) {
	raw := `Post "https://ark.cn-beijing.volces.com/api/coding/v3/chat/completions": context deadline exceeded`
	got := redactURLs(raw)
	want := `Post "[redacted-url]": context deadline exceeded`
	if got != want {
		t.Fatalf("unexpected redaction result: got %q, want %q", got, want)
	}
}
