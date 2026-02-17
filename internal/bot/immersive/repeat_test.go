package immersive

import "testing"

func TestDetectConsecutiveRepeat_TwoSpeakers(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "开冲", speaker: "name=alice;id=1"},
		{text: "开冲", speaker: "name=bob;id=2"},
	})
	if text != "开冲" {
		t.Fatalf("expected repeat text 开冲, got %q", text)
	}
	if count != 2 {
		t.Fatalf("expected repeat count 2, got %d", count)
	}
	if participants != 2 {
		t.Fatalf("expected participants 2, got %d", participants)
	}
}

func TestDetectConsecutiveRepeat_SameSpeakerOnly(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "1", speaker: "name=alice;id=1"},
		{text: "1", speaker: "name=alice;id=1"},
	})
	if text != "" || count != 0 || participants != 0 {
		t.Fatalf("expected no repeat trigger, got text=%q count=%d participants=%d", text, count, participants)
	}
}

func TestDetectConsecutiveRepeat_RequiresConsecutive(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "喵", speaker: "name=alice;id=1"},
		{text: "不是喵", speaker: "name=bob;id=2"},
		{text: "喵", speaker: "name=carol;id=3"},
	})
	if text != "" || count != 0 || participants != 0 {
		t.Fatalf("expected no repeat trigger, got text=%q count=%d participants=%d", text, count, participants)
	}
}

func TestDetectConsecutiveRepeat_UsesLatestRun(t *testing.T) {
	text, count, participants := detectConsecutiveRepeat([]queuedMessage{
		{text: "第一段", speaker: "name=alice;id=1"},
		{text: "第一段", speaker: "name=bob;id=2"},
		{text: "第二段", speaker: "name=carol;id=3"},
		{text: "第二段", speaker: "name=dave;id=4"},
		{text: "第二段", speaker: "name=erin;id=5"},
	})
	if text != "第二段" {
		t.Fatalf("expected latest repeated text 第二段, got %q", text)
	}
	if count != 3 {
		t.Fatalf("expected repeat count 3, got %d", count)
	}
	if participants != 3 {
		t.Fatalf("expected participants 3, got %d", participants)
	}
}

func TestNormalizeRepeatText_CompressesWhitespace(t *testing.T) {
	normalized := normalizeRepeatText("  我  喜欢\t猫咪 \n")
	if normalized != "我 喜欢 猫咪" {
		t.Fatalf("unexpected normalized text %q", normalized)
	}
}
