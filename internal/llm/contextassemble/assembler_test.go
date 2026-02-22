package contextassemble

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

func TestAssembler_AssembleBuilds50Plus50Windows(t *testing.T) {
	chatStore := chatlog.NewMemoryStore()
	diaryStore := diary.NewMemoryStore()
	assembler := New(chatStore, diaryStore, Options{})
	sessionKey := "group:100"
	base := time.Date(2026, 2, 21, 8, 0, 0, 0, time.UTC)

	for i := 1; i <= 60; i++ {
		err := chatStore.Append(context.Background(), sessionKey, chatlog.Entry{
			Role:      chatlog.RoleUser,
			Speaker:   "alice",
			Content:   fmt.Sprintf("chat-%02d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("append chat %d failed: %v", i, err)
		}
	}

	for i := 1; i <= 55; i++ {
		_, err := diaryStore.Write(context.Background(), sessionKey, diary.Entry{
			Author:    "assistant",
			Content:   fmt.Sprintf("diary-%02d", i),
			CreatedAt: base.Add(time.Duration(i) * time.Hour),
		})
		if err != nil {
			t.Fatalf("write diary %d failed: %v", i, err)
		}
	}

	result, err := assembler.Assemble(context.Background(), Request{
		SessionKey: sessionKey,
		Meta: Meta{
			Now:               " 2026-02-21 08:00:00 ",
			AssistantIdentity: " name=neko;id=10000 ",
			BotConfigNames:    []string{"neko", "mimi", "neko"},
			SessionType:       " group ",
		},
	})
	if err != nil {
		t.Fatalf("assemble failed: %v", err)
	}

	assertBlockNames(t, result.Blocks, []string{BlockRecentChat, BlockRecentDiary, BlockMeta})

	recentChat := mustBlock(t, result, BlockRecentChat)
	recentDiary := mustBlock(t, result, BlockRecentDiary)
	meta := mustBlock(t, result, BlockMeta)

	chatLines := nonEmptyLines(recentChat.Content)
	if len(chatLines) != 50 {
		t.Fatalf("recent_chat line count mismatch: got %d, want 50", len(chatLines))
	}
	if !strings.Contains(chatLines[0], "chat-11") {
		t.Fatalf("recent_chat should keep last 50 entries from chat-11, got first line %q", chatLines[0])
	}
	if !strings.Contains(chatLines[len(chatLines)-1], "chat-60") {
		t.Fatalf("recent_chat should end with latest entry chat-60, got last line %q", chatLines[len(chatLines)-1])
	}

	diaryLines := nonEmptyLines(recentDiary.Content)
	if len(diaryLines) != 50 {
		t.Fatalf("recent_diary line count mismatch: got %d, want 50", len(diaryLines))
	}
	if !strings.Contains(diaryLines[0], "diary-06") {
		t.Fatalf("recent_diary should keep last 50 entries from diary-06, got first line %q", diaryLines[0])
	}
	if !strings.Contains(diaryLines[len(diaryLines)-1], "diary-55") {
		t.Fatalf("recent_diary should end with latest entry diary-55, got last line %q", diaryLines[len(diaryLines)-1])
	}

	if !strings.Contains(meta.Content, "now=2026-02-21 08:00:00") {
		t.Fatalf("meta now mismatch: %q", meta.Content)
	}
	if !strings.Contains(meta.Content, "assistant_identity=name=neko;id=10000") {
		t.Fatalf("meta assistant_identity mismatch: %q", meta.Content)
	}
	if !strings.Contains(meta.Content, "bot_config_names=[mimi,neko]") {
		t.Fatalf("meta bot_config_names mismatch: %q", meta.Content)
	}
	if !strings.Contains(meta.Content, "session_type=group") {
		t.Fatalf("meta session_type mismatch: %q", meta.Content)
	}
	if recentChat.Truncated || recentDiary.Truncated || meta.Truncated {
		t.Fatalf("no block should be truncated when max chars is not set")
	}
	if result.TotalChars != totalChars(result.Blocks) {
		t.Fatalf("total chars mismatch: got %d, want %d", result.TotalChars, totalChars(result.Blocks))
	}
}

func TestAssembler_AssemblePredictableClipping(t *testing.T) {
	chatStore := chatlog.NewMemoryStore()
	diaryStore := diary.NewMemoryStore()
	assembler := New(chatStore, diaryStore, Options{})
	sessionKey := "group:clip"

	if err := chatStore.Append(context.Background(), sessionKey, chatlog.Entry{Content: strings.Repeat("A", 10)}); err != nil {
		t.Fatalf("append chat failed: %v", err)
	}
	if _, err := diaryStore.Write(context.Background(), sessionKey, diary.Entry{Content: strings.Repeat("B", 10)}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	metaReq := Meta{
		Now:               "2026-02-22 10:00:00",
		AssistantIdentity: "name=assistant;id=1",
		BotConfigNames:    []string{"neko"},
		SessionType:       "group",
	}
	base, err := assembler.Assemble(context.Background(), Request{
		SessionKey: sessionKey,
		Meta:       metaReq,
	})
	if err != nil {
		t.Fatalf("assemble baseline failed: %v", err)
	}
	baseChat := mustBlock(t, base, BlockRecentChat).Content
	baseDiary := mustBlock(t, base, BlockRecentDiary).Content
	baseMeta := mustBlock(t, base, BlockMeta).Content
	baseTotal := base.TotalChars
	chatChars := charCount(baseChat)
	diaryChars := charCount(baseDiary)
	metaChars := charCount(baseMeta)

	tests := []struct {
		name         string
		maxChars     int
		wantChat     string
		wantDiary    string
		wantMeta     string
		wantChatCut  bool
		wantDiaryCut bool
		wantMetaCut  bool
	}{
		{
			name:      "no clipping",
			maxChars:  baseTotal,
			wantChat:  baseChat,
			wantDiary: baseDiary,
			wantMeta:  baseMeta,
		},
		{
			name:        "clip meta first",
			maxChars:    baseTotal - 2,
			wantChat:    baseChat,
			wantDiary:   baseDiary,
			wantMeta:    truncateByChars(baseMeta, metaChars-2),
			wantMetaCut: true,
		},
		{
			name:         "clip meta then recent_diary",
			maxChars:     chatChars + diaryChars - 1,
			wantChat:     baseChat,
			wantDiary:    truncateByChars(baseDiary, diaryChars-1),
			wantMeta:     "",
			wantDiaryCut: true,
			wantMetaCut:  true,
		},
		{
			name:         "clip all blocks in order",
			maxChars:     chatChars - 1,
			wantChat:     truncateByChars(baseChat, chatChars-1),
			wantDiary:    "",
			wantMeta:     "",
			wantChatCut:  true,
			wantDiaryCut: true,
			wantMetaCut:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := assembler.Assemble(context.Background(), Request{
				SessionKey: sessionKey,
				Meta:       metaReq,
				MaxChars:   tc.maxChars,
			})
			if err != nil {
				t.Fatalf("assemble failed: %v", err)
			}

			recentChat := mustBlock(t, result, BlockRecentChat)
			recentDiary := mustBlock(t, result, BlockRecentDiary)
			meta := mustBlock(t, result, BlockMeta)

			if recentChat.Content != tc.wantChat {
				t.Fatalf("recent_chat mismatch: got %q, want %q", recentChat.Content, tc.wantChat)
			}
			if recentDiary.Content != tc.wantDiary {
				t.Fatalf("recent_diary mismatch: got %q, want %q", recentDiary.Content, tc.wantDiary)
			}
			if meta.Content != tc.wantMeta {
				t.Fatalf("meta mismatch: got %q, want %q", meta.Content, tc.wantMeta)
			}
			if recentChat.Truncated != tc.wantChatCut {
				t.Fatalf("recent_chat truncated mismatch: got %v, want %v", recentChat.Truncated, tc.wantChatCut)
			}
			if recentDiary.Truncated != tc.wantDiaryCut {
				t.Fatalf("recent_diary truncated mismatch: got %v, want %v", recentDiary.Truncated, tc.wantDiaryCut)
			}
			if meta.Truncated != tc.wantMetaCut {
				t.Fatalf("meta truncated mismatch: got %v, want %v", meta.Truncated, tc.wantMetaCut)
			}
			if result.TotalChars > tc.maxChars {
				t.Fatalf("total chars should not exceed max: got %d, max %d", result.TotalChars, tc.maxChars)
			}
		})
	}
}

func TestAssembler_AssembleValidationAndRequestOverride(t *testing.T) {
	_, err := New(nil, diary.NewMemoryStore(), Options{}).Assemble(context.Background(), Request{SessionKey: "s"})
	if err != ErrNilChatStore {
		t.Fatalf("expected ErrNilChatStore, got %v", err)
	}

	_, err = New(chatlog.NewMemoryStore(), nil, Options{}).Assemble(context.Background(), Request{SessionKey: "s"})
	if err != ErrNilDiaryStore {
		t.Fatalf("expected ErrNilDiaryStore, got %v", err)
	}

	assembler := New(chatlog.NewMemoryStore(), diary.NewMemoryStore(), Options{MaxChars: 8})
	sessionKey := "group:override"
	if err := assembler.chatStore.Append(context.Background(), sessionKey, chatlog.Entry{Content: strings.Repeat("A", 10)}); err != nil {
		t.Fatalf("append chat failed: %v", err)
	}
	if _, err := assembler.diaryStore.Write(context.Background(), sessionKey, diary.Entry{Content: strings.Repeat("B", 10)}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	_, err = assembler.Assemble(context.Background(), Request{SessionKey: "   "})
	if err != ErrEmptySessionKey {
		t.Fatalf("expected ErrEmptySessionKey, got %v", err)
	}

	clipped, err := assembler.Assemble(context.Background(), Request{
		SessionKey: sessionKey,
		Meta: Meta{
			Now:               strings.Repeat("C", 10),
			AssistantIdentity: "assistant",
			BotConfigNames:    []string{"neko"},
			SessionType:       "group",
		},
	})
	if err != nil {
		t.Fatalf("assemble with default max chars failed: %v", err)
	}
	if clipped.TotalChars != 8 {
		t.Fatalf("expected option max chars to clip to 8, got %d", clipped.TotalChars)
	}

	overridden, err := assembler.Assemble(context.Background(), Request{
		SessionKey: sessionKey,
		Meta: Meta{
			Now:               strings.Repeat("C", 10),
			AssistantIdentity: "assistant",
			BotConfigNames:    []string{"neko"},
			SessionType:       "group",
		},
		MaxChars: 30,
	})
	if err != nil {
		t.Fatalf("assemble with request max chars failed: %v", err)
	}
	if overridden.TotalChars != 30 {
		t.Fatalf("expected request max chars override to keep 30, got %d", overridden.TotalChars)
	}
}

func mustBlock(t *testing.T, result Result, name string) Block {
	t.Helper()
	block, ok := result.Block(name)
	if !ok {
		t.Fatalf("missing block: %s", name)
	}
	return block
}

func nonEmptyLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

func assertBlockNames(t *testing.T, blocks []Block, want []string) {
	t.Helper()
	if len(blocks) != len(want) {
		t.Fatalf("block count mismatch: got %d, want %d", len(blocks), len(want))
	}
	for i := range want {
		if blocks[i].Name != want[i] {
			t.Fatalf("block name mismatch at %d: got %q, want %q", i, blocks[i].Name, want[i])
		}
	}
}
