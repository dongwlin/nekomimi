package immersive

import (
	"testing"
	"time"
)

func TestClassifyNicknamePosition(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		nick     string
		expected NicknamePosition
	}{
		{"isolated name", "neko", "neko", NickIsolated},
		{"isolated with trailing punct", "neko！", "neko", NickIsolated},
		{"isolated with surrounding punct", "「neko」", "neko", NickIsolated},
		{"start of sentence", "neko 帮我看看", "neko", NickStart},
		{"start with leading punct", "「neko 帮我看看」", "neko", NickStart},
		{"end of sentence", "来问问neko", "neko", NickEnd},
		{"end with trailing punct", "来问问neko？", "neko", NickEnd},
		{"middle of sentence", "我觉得neko说得对", "neko", NickMiddle},
		{"not found", "你好世界", "neko", NickMiddle}, // classifyNicknamePosition is only called when Contains==true
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyNicknamePosition(tt.text, tt.nick)
			if got != tt.expected {
				t.Fatalf("classifyNicknamePosition(%q, %q) = %d, want %d", tt.text, tt.nick, got, tt.expected)
			}
		})
	}
}

func TestDetectNicknamePosition(t *testing.T) {
	buffer := &ImmersiveBuffer{
		nicknames: []string{"Neko"},
	}

	tests := []struct {
		name     string
		text     string
		expected NicknamePosition
	}{
		{"no match", "你好世界", NickNotFound},
		{"isolated", "Neko", NickIsolated},
		{"isolated case insensitive", "neko", NickIsolated},
		{"start", "Neko 帮我看看", NickStart},
		{"end", "帮我问一下Neko", NickEnd},
		{"middle", "我觉得Neko说得对", NickMiddle},
		{"empty text", "", NickNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buffer.detectNicknamePosition(tt.text)
			if got != tt.expected {
				t.Fatalf("detectNicknamePosition(%q) = %d, want %d", tt.text, got, tt.expected)
			}
		})
	}
}

func TestBandFromScore(t *testing.T) {
	tests := []struct {
		score    int
		expected SignalBand
	}{
		{-5, BandIgnore},
		{0, BandIgnore},
		{1, BandIgnore},
		{2, BandObserve},
		{4, BandObserve},
		{5, BandEngage},
		{9, BandEngage},
		{10, BandHighPriority},
		{20, BandHighPriority},
	}

	for _, tt := range tests {
		got := bandFromScore(tt.score)
		if got != tt.expected {
			t.Fatalf("bandFromScore(%d) = %q, want %q", tt.score, got, tt.expected)
		}
	}
}

func TestScoreSignals_ExplicitMentionIsHighPriority(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount:  1,
		MentionsToBot:  1,
		AddressedToBot: 1,
		LastSpeaker:    "name=alice",
	}
	snapshot := behaviorSnapshot{
		Mode:       ModeIdle,
		EnergyBand: "medium",
	}

	score := scoreSignals("group:1", meta, snapshot, now)
	if score.Band != BandHighPriority {
		t.Fatalf("explicit @bot should be high_priority, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_MiddleNicknameNotStrongCall(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount:       1,
		AddressedToBot:      1,
		NicknameMiddleCount: 1,
		LastSpeaker:         "name=bob",
	}
	snapshot := behaviorSnapshot{
		Mode:       ModeIdle,
		EnergyBand: "medium",
	}

	score := scoreSignals("group:1", meta, snapshot, now)
	if score.Band == BandHighPriority {
		t.Fatalf("middle nickname should not be high_priority, got band=%q score=%d", score.Band, score.TotalScore)
	}
	if score.Band == BandEngage {
		t.Fatalf("middle nickname alone should not be engage, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_CoolingDownWeakQuestionIgnored(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount:  1,
		QuestionsCount: 1,
		LastSpeaker:    "name=bob",
	}
	snapshot := behaviorSnapshot{
		Mode:           ModeCoolingDown,
		EnergyBand:     "low",
		LastBotReplyAt: now.Add(-20 * time.Second),
	}

	score := scoreSignals("group:1", meta, snapshot, now)
	if score.Band == BandHighPriority || score.Band == BandEngage {
		t.Fatalf("cooling down weak question should not be high_priority or engage, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_FocusSpeakerContinuousQuestionScoreRises(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)

	meta1 := queueMeta{
		MessagesCount:      1,
		AddressedToBot:     1,
		QuestionsCount:     1,
		DirectedQuestions:  1,
		NicknameStartCount: 1,
		LastSpeaker:        "name=alice",
	}
	snapshot1 := behaviorSnapshot{
		Mode:         ModeAddressed,
		FocusSpeaker: "name=alice",
	}
	score1 := scoreSignals("group:1", meta1, snapshot1, now)

	meta2 := queueMeta{
		MessagesCount:      2,
		AddressedToBot:     2,
		QuestionsCount:     2,
		DirectedQuestions:  2,
		NicknameStartCount: 2,
		LastSpeaker:        "name=alice",
	}
	snapshot2 := behaviorSnapshot{
		Mode:            ModeInThread,
		FocusSpeaker:    "name=alice",
		PendingQuestion: true,
	}
	score2 := scoreSignals("group:1", meta2, snapshot2, now)

	if score2.TotalScore <= score1.TotalScore {
		t.Fatalf("focus speaker continuing should increase score: first=%d second=%d", score1.TotalScore, score2.TotalScore)
	}
}

func TestScoreSignals_PrivateSessionHighPriority(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount: 1,
		LastSpeaker:   "name=alice",
	}
	snapshot := behaviorSnapshot{
		Mode: ModeIdle,
	}

	score := scoreSignals("private:alice", meta, snapshot, now)
	if score.Band != BandHighPriority {
		t.Fatalf("private session should be high_priority, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_NicknameStartIsEngage(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount:      1,
		AddressedToBot:     1,
		NicknameStartCount: 1,
		LastSpeaker:        "name=alice",
	}
	snapshot := behaviorSnapshot{
		Mode: ModeIdle,
	}

	score := scoreSignals("group:1", meta, snapshot, now)
	if score.Band != BandEngage {
		t.Fatalf("nickname at start should be engage, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_NicknameEndIsObserve(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)
	meta := queueMeta{
		MessagesCount:    1,
		AddressedToBot:   1,
		NicknameEndCount: 1,
		LastSpeaker:      "name=alice",
	}
	snapshot := behaviorSnapshot{
		Mode: ModeIdle,
	}

	score := scoreSignals("group:1", meta, snapshot, now)
	if score.Band != BandObserve {
		t.Fatalf("nickname at end should be observe, got band=%q score=%d", score.Band, score.TotalScore)
	}
}

func TestScoreSignals_DirectedQuestionStrongerThanAmbient(t *testing.T) {
	now := time.Date(2026, 3, 6, 12, 0, 0, 0, time.Local)

	metaDirected := queueMeta{
		MessagesCount:      1,
		AddressedToBot:     1,
		QuestionsCount:     1,
		DirectedQuestions:  1,
		NicknameStartCount: 1,
		LastSpeaker:        "name=alice",
	}
	metaAmbient := queueMeta{
		MessagesCount:  1,
		QuestionsCount: 1,
		LastSpeaker:    "name=bob",
	}
	snapshot := behaviorSnapshot{Mode: ModeIdle}

	scoreDirected := scoreSignals("group:1", metaDirected, snapshot, now)
	scoreAmbient := scoreSignals("group:1", metaAmbient, snapshot, now)

	if scoreDirected.TotalScore <= scoreAmbient.TotalScore {
		t.Fatalf("directed question should score higher than ambient: directed=%d ambient=%d",
			scoreDirected.TotalScore, scoreAmbient.TotalScore)
	}
}

func TestFormatSignalFeatures(t *testing.T) {
	features := []SignalFeature{
		{Name: "explicit_mention", Points: 10},
		{Name: "mode_cooling", Points: -4},
	}
	got := formatSignalFeatures(features)
	if got != "explicit_mention(+10), mode_cooling(-4)" {
		t.Fatalf("unexpected format: %q", got)
	}
}

func TestFormatSignalFeatures_Empty(t *testing.T) {
	got := formatSignalFeatures(nil)
	if got != "" {
		t.Fatalf("expected empty string for nil features, got %q", got)
	}
}
