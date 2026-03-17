package llm

import (
	"regexp"
	"strings"
)

var urlInErrorPattern = regexp.MustCompile(`https?://[^\s"]+`)

// UserVisibleError returns a sanitized message safe for user-facing replies.
func UserVisibleError(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "未知错误"
	}
	return urlInErrorPattern.ReplaceAllString(msg, "[redacted-url]")
}
