package immersive

import "testing"

func TestLooksLikeQuestion_WithQuestionMark(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"你好吗？", true},
		{"你在吗?", true},
		{"Hello?", true},
		{"你好", false},
		{"这是一个陈述句", false},
		{"", false},
	}

	for _, tt := range tests {
		result := looksLikeQuestion(tt.text)
		if result != tt.expected {
			t.Errorf("looksLikeQuestion(%q) = %v, expected %v", tt.text, result, tt.expected)
		}
	}
}

func TestLooksLikeQuestion_WithChineseQuestionWords(t *testing.T) {
	tests := []struct {
		text     string
		expected bool
	}{
		{"你好吗", true},
		{"能帮我吗", true},
		{"能否告诉我", true},
		{"能否", true},
		{"你好", false},
		{"这是个陈述句", false},
	}

	for _, tt := range tests {
		result := looksLikeQuestion(tt.text)
		if result != tt.expected {
			t.Errorf("looksLikeQuestion(%q) = %v, expected %v", tt.text, result, tt.expected)
		}
	}
}

func TestContainsNickname(t *testing.T) {
	buffer := &ImmersiveBuffer{
		nicknames: []string{"Alice", "Bob", "测试"},
	}

	tests := []struct {
		text     string
		expected bool
	}{
		{"你好Alice", true},
		{"hello bob", true},
		{"你好测试", true},
		{"你好", false},
		{"没有昵称", false},
		{"", false},
	}

	for _, tt := range tests {
		result := buffer.containsNickname(tt.text)
		if result != tt.expected {
			t.Errorf("containsNickname(%q) = %v, expected %v", tt.text, result, tt.expected)
		}
	}
}

func TestContainsNickname_EmptyNicknames(t *testing.T) {
	buffer := &ImmersiveBuffer{
		nicknames: []string{},
	}

	result := buffer.containsNickname("Hello Alice")
	if result != false {
		t.Errorf("expected false for empty nicknames, got %v", result)
	}
}

func TestContainsNickname_TrimsWhitespace(t *testing.T) {
	buffer := &ImmersiveBuffer{
		nicknames: []string{"  Alice  "},
	}

	result := buffer.containsNickname("hello alice")
	if result != true {
		t.Errorf("expected true for trimmed nickname, got %v", result)
	}
}
