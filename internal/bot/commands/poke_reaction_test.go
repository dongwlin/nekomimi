package commands

import (
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
)

func TestResolvePokeMoodTier_Boundaries(t *testing.T) {
	mild := 3
	annoyed := 6
	tests := []struct {
		count int
		want  pokeMoodTier
	}{
		{1, pokeMoodWarm},
		{2, pokeMoodWarm},
		{3, pokeMoodMild},
		{5, pokeMoodMild},
		{6, pokeMoodAnnoyed},
	}
	for _, tt := range tests {
		got := resolvePokeMoodTier(tt.count, mild, annoyed)
		if got != tt.want {
			t.Fatalf("count=%d got=%s want=%s", tt.count, got, tt.want)
		}
	}
}

func TestPokeTrackerObserve_WindowPrune(t *testing.T) {
	tracker := newPokeTracker(config.PokeReactionConfig{
		WindowMS:         1000,
		MildThreshold:    3,
		AnnoyedThreshold: 6,
	})
	base := time.Date(2026, 2, 18, 1, 2, 3, 0, time.UTC)
	session := "group:1"
	user := "u1"

	count, mood := tracker.Observe(session, user, base)
	if count != 1 || mood != pokeMoodWarm {
		t.Fatalf("first observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, user, base.Add(400*time.Millisecond))
	if count != 2 || mood != pokeMoodWarm {
		t.Fatalf("second observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, user, base.Add(800*time.Millisecond))
	if count != 3 || mood != pokeMoodMild {
		t.Fatalf("third observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, user, base.Add(2100*time.Millisecond))
	if count != 1 || mood != pokeMoodWarm {
		t.Fatalf("window prune expected reset to 1, got count=%d mood=%s", count, mood)
	}
}

func TestBuildPokeReplyPrompt_ContainsKeyConstraints(t *testing.T) {
	prompt := buildPokeReplyPrompt(4, pokeMoodMild, 18)
	for _, token := range []string{
		"连续戳了 4 次",
		"不超过18字",
		"轻微烦躁",
		"想和你对话",
	} {
		if !strings.Contains(prompt, token) {
			t.Fatalf("prompt missing token %q: %s", token, prompt)
		}
	}
}
