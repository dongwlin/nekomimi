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

	count, mood := tracker.Observe(session, base)
	if count != 1 || mood != pokeMoodWarm {
		t.Fatalf("first observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, base.Add(400*time.Millisecond))
	if count != 2 || mood != pokeMoodWarm {
		t.Fatalf("second observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, base.Add(800*time.Millisecond))
	if count != 3 || mood != pokeMoodMild {
		t.Fatalf("third observe got count=%d mood=%s", count, mood)
	}
	count, mood = tracker.Observe(session, base.Add(2100*time.Millisecond))
	if count != 1 || mood != pokeMoodWarm {
		t.Fatalf("window prune expected reset to 1, got count=%d mood=%s", count, mood)
	}
}

func TestPokeTrackerObserve_CountBySessionOnly(t *testing.T) {
	tracker := newPokeTracker(config.PokeReactionConfig{
		WindowMS:         5000,
		MildThreshold:    3,
		AnnoyedThreshold: 6,
	})
	base := time.Date(2026, 2, 18, 1, 2, 3, 0, time.UTC)

	count, _ := tracker.Observe("group:1", base)
	if count != 1 {
		t.Fatalf("expected first count=1, got %d", count)
	}
	count, _ = tracker.Observe("group:1", base.Add(200*time.Millisecond))
	if count != 2 {
		t.Fatalf("expected same-session count=2, got %d", count)
	}
	count, _ = tracker.Observe("group:2", base.Add(400*time.Millisecond))
	if count != 1 {
		t.Fatalf("expected different-session count reset to 1, got %d", count)
	}
}

func TestBuildPokeReplyPrompt_ContainsKeyConstraints(t *testing.T) {
	prompt := buildPokeReplyPrompt(4, pokeMoodMild)
	for _, token := range []string{
		"连续被戳了 4 次",
		"轻微烦躁",
		"优先 1-3 句",
		"\\n---\\n",
	} {
		if !strings.Contains(prompt, token) {
			t.Fatalf("prompt missing token %q: %s", token, prompt)
		}
	}
}
