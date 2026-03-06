package immersive

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestDetectMessageSignals_Addressed(t *testing.T) {
	buffer := &ImmersiveBuffer{
		nicknames: []string{"Neko"},
	}

	tests := []struct {
		name          string
		ctx           *zero.Ctx
		text          string
		wantMention   bool
		wantAddressed bool
		wantQuestion  bool
	}{
		{
			name:          "other user handle does not address bot",
			text:          "联系 @someone_else 处理一下",
			wantMention:   false,
			wantAddressed: false,
			wantQuestion:  false,
		},
		{
			name:          "email address does not address bot",
			text:          "我的邮箱是 user@example.com",
			wantMention:   false,
			wantAddressed: false,
			wantQuestion:  false,
		},
		{
			name:          "bot nickname marks addressed",
			text:          "Neko 帮我记一下",
			wantMention:   false,
			wantAddressed: true,
			wantQuestion:  false,
		},
		{
			name: "explicit at mention marks mention and addressed",
			ctx: &zero.Ctx{Event: &zero.Event{
				SelfID: 12345,
				Message: message.Message{
					{Type: "at", Data: map[string]string{"qq": "12345"}},
				},
			}},
			text:          "帮我看看",
			wantMention:   true,
			wantAddressed: true,
			wantQuestion:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mention, addressed, question := buffer.detectMessageSignals(tt.ctx, tt.text)
			if mention != tt.wantMention {
				t.Fatalf("mention = %v, want %v", mention, tt.wantMention)
			}
			if addressed != tt.wantAddressed {
				t.Fatalf("addressed = %v, want %v", addressed, tt.wantAddressed)
			}
			if question != tt.wantQuestion {
				t.Fatalf("question = %v, want %v", question, tt.wantQuestion)
			}
		})
	}
}

func TestLooksLikeQuestion(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{name: "chinese question mark", text: "你好吗？", expected: true},
		{name: "english question mark", text: "Hello?", expected: true},
		{name: "ma indicator", text: "你好吗", expected: true},
		{name: "nengfou indicator", text: "能否告诉我", expected: true},
		{name: "zenme indicator", text: "这是怎么回事", expected: true},
		{name: "shei indicator", text: "你是谁", expected: true},
		{name: "youmeiyou indicator", text: "有没有人在", expected: true},
		{name: "kebukeyi indicator", text: "这个功能可不可以关闭", expected: true},
		{name: "duoshao indicator", text: "这个功能要多少内存", expected: true},
		{name: "duojiu indicator", text: "还要多久完成", expected: true},
		{name: "nali indicator", text: "日志放在哪里", expected: true},
		{name: "ne indicator", text: "现在怎么办呢", expected: true},
		{name: "statement remains false", text: "好的我知道了", expected: false},
		{name: "plain greeting remains false", text: "你好", expected: false},
		{name: "empty string remains false", text: "", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := looksLikeQuestion(tt.text)
			if result != tt.expected {
				t.Fatalf("looksLikeQuestion(%q) = %v, expected %v", tt.text, result, tt.expected)
			}
		})
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
