package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

func TestRouter_InternalProvider_ListAndCall(t *testing.T) {
	chatStore := chatlog.NewMemoryStore()
	diaryStore := diary.NewMemoryStore()
	sessionKey := "group:tool-test"

	if err := chatStore.Append(context.Background(), sessionKey, chatlog.Entry{
		Role:    chatlog.RoleUser,
		Speaker: "alice",
		Content: "hello from chat",
	}); err != nil {
		t.Fatalf("append chat failed: %v", err)
	}
	if _, err := diaryStore.Write(context.Background(), sessionKey, diary.Entry{
		Author:  "assistant",
		Content: "remember this fact",
	}); err != nil {
		t.Fatalf("write diary failed: %v", err)
	}

	router := NewRouter()
	if err := router.Register(InternalProviderName, NewInternalProvider(chatStore, diaryStore, InternalProviderOptions{})); err != nil {
		t.Fatalf("register internal provider failed: %v", err)
	}

	tools, err := router.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools failed: %v", err)
	}
	assertToolNames(t, tools, []string{
		ToolReadChatHistory,
		ToolReadDiary,
		ToolWriteDiary,
	})

	readChat, err := router.CallTool(context.Background(), CallRequest{
		Name:      ToolReadChatHistory,
		Arguments: mustRawJSON(t, map[string]any{"session_key": sessionKey, "limit": 1}),
	})
	if err != nil {
		t.Fatalf("call read chat failed: %v", err)
	}
	if readChat.IsError {
		t.Fatalf("read chat should not fail: %+v", readChat.Error)
	}
	if !strings.Contains(readChat.Content, "hello from chat") {
		t.Fatalf("unexpected read chat content: %q", readChat.Content)
	}

	writeDiary, err := router.CallTool(context.Background(), CallRequest{
		Name: ToolWriteDiary,
		Arguments: mustRawJSON(t, map[string]any{
			"session_key": sessionKey,
			"content":     "new diary note",
			"tags":        []string{"todo"},
		}),
	})
	if err != nil {
		t.Fatalf("call write diary failed: %v", err)
	}
	if writeDiary.IsError {
		t.Fatalf("write diary should not fail: %+v", writeDiary.Error)
	}
	if !strings.Contains(writeDiary.Content, "saved diary entry") {
		t.Fatalf("unexpected write diary content: %q", writeDiary.Content)
	}

	readDiary, err := router.CallTool(context.Background(), CallRequest{
		Name:      ToolReadDiary,
		Arguments: mustRawJSON(t, map[string]any{"session_key": sessionKey, "limit": 2}),
	})
	if err != nil {
		t.Fatalf("call read diary failed: %v", err)
	}
	if readDiary.IsError {
		t.Fatalf("read diary should not fail: %+v", readDiary.Error)
	}
	if !strings.Contains(readDiary.Content, "new diary note") {
		t.Fatalf("unexpected read diary content: %q", readDiary.Content)
	}
}

func TestInternalProvider_ValidationAndTruncation(t *testing.T) {
	chatStore := chatlog.NewMemoryStore()
	diaryStore := diary.NewMemoryStore()
	sessionKey := "group:truncate"

	if err := chatStore.Append(context.Background(), sessionKey, chatlog.Entry{
		Role:    chatlog.RoleUser,
		Content: strings.Repeat("A", 40),
	}); err != nil {
		t.Fatalf("append chat failed: %v", err)
	}

	provider := NewInternalProvider(chatStore, diaryStore, InternalProviderOptions{
		MaxResultChars: 12,
	})

	invalidArgs, err := provider.CallTool(context.Background(), CallRequest{
		Name:      ToolReadChatHistory,
		Arguments: mustRawJSON(t, map[string]any{}),
	})
	if err != nil {
		t.Fatalf("call with invalid args failed: %v", err)
	}
	assertErrorCode(t, invalidArgs, ErrorCodeInvalidArguments)

	notFound, err := provider.CallTool(context.Background(), CallRequest{
		Name: "internal/not_exists",
	})
	if err != nil {
		t.Fatalf("call unknown tool failed: %v", err)
	}
	assertErrorCode(t, notFound, ErrorCodeNotFound)

	readChat, err := provider.CallTool(context.Background(), CallRequest{
		Name:      ToolReadChatHistory,
		Arguments: mustRawJSON(t, map[string]any{"session_key": sessionKey}),
	})
	if err != nil {
		t.Fatalf("call read chat failed: %v", err)
	}
	if readChat.IsError {
		t.Fatalf("read chat should not fail: %+v", readChat.Error)
	}
	if runeCount(readChat.Content) > 12 {
		t.Fatalf("content should be truncated to <=12 chars, got %d", runeCount(readChat.Content))
	}
	structured := decodeObjectMap(t, readChat.Structured)
	if truncated, _ := structured["truncated"].(bool); !truncated {
		t.Fatalf("structured result should mark truncation")
	}

	invalidWrite, err := provider.CallTool(context.Background(), CallRequest{
		Name: ToolWriteDiary,
		Arguments: mustRawJSON(t, map[string]any{
			"session_key": sessionKey,
			"content":     "   ",
		}),
	})
	if err != nil {
		t.Fatalf("call write diary invalid args failed: %v", err)
	}
	assertErrorCode(t, invalidWrite, ErrorCodeInvalidArguments)
}

func TestRouter_MapsProviderErrorToDiagnosableResult(t *testing.T) {
	router := NewRouter()
	errProvider := &stubProvider{
		list: []Descriptor{{Name: "boom/tool"}},
		call: func(ctx context.Context, req CallRequest) (CallResult, error) {
			return CallResult{}, errors.New("boom call failed")
		},
	}
	if err := router.Register("boom", errProvider); err != nil {
		t.Fatalf("register provider failed: %v", err)
	}

	result, err := router.CallTool(context.Background(), CallRequest{Name: "boom/tool"})
	if err != nil {
		t.Fatalf("router call failed: %v", err)
	}
	assertErrorCode(t, result, ErrorCodeInternal)
	if !strings.Contains(result.Error.Message, "boom call failed") {
		t.Fatalf("error message should include provider failure, got %q", result.Error.Message)
	}
}

func TestRouter_ListToolsDuplicateName(t *testing.T) {
	router := NewRouter()
	if err := router.Register("p1", &stubProvider{list: []Descriptor{{Name: "dup/tool"}}}); err != nil {
		t.Fatalf("register p1 failed: %v", err)
	}
	if err := router.Register("p2", &stubProvider{list: []Descriptor{{Name: "dup/tool"}}}); err != nil {
		t.Fatalf("register p2 failed: %v", err)
	}

	_, err := router.ListTools(context.Background())
	if err == nil {
		t.Fatalf("list tools should fail on duplicate names")
	}
	if !strings.Contains(err.Error(), "duplicate tool name") {
		t.Fatalf("unexpected duplicate error: %v", err)
	}
}

type stubProvider struct {
	list []Descriptor
	call func(ctx context.Context, req CallRequest) (CallResult, error)
}

func (s *stubProvider) ListTools(ctx context.Context) ([]Descriptor, error) {
	return append([]Descriptor(nil), s.list...), nil
}

func (s *stubProvider) CallTool(ctx context.Context, req CallRequest) (CallResult, error) {
	if s.call == nil {
		return errorResult(req.Name, ErrorCodeNotFound, "tool not found", false), nil
	}
	return s.call(ctx, req)
}

func assertToolNames(t *testing.T, tools []Descriptor, want []string) {
	t.Helper()
	if len(tools) != len(want) {
		t.Fatalf("tool count mismatch: got %d, want %d", len(tools), len(want))
	}
	for i := range want {
		if tools[i].Name != want[i] {
			t.Fatalf("tool name mismatch at %d: got %q, want %q", i, tools[i].Name, want[i])
		}
	}
}

func assertErrorCode(t *testing.T, result CallResult, want ErrorCode) {
	t.Helper()
	if !result.IsError {
		t.Fatalf("expected error result, got success")
	}
	if result.Error == nil {
		t.Fatalf("expected error payload")
	}
	if result.Error.Code != want {
		t.Fatalf("error code mismatch: got %q, want %q", result.Error.Code, want)
	}
	if strings.TrimSpace(result.Error.Message) == "" {
		t.Fatalf("error message should not be empty")
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal args failed: %v", err)
	}
	return data
}

func decodeObjectMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	result := make(map[string]any)
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode structured json failed: %v", err)
	}
	return result
}

func runeCount(value string) int {
	return len([]rune(value))
}
