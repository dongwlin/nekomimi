package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

var (
	readChatHistoryInputSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["session_key"],
  "properties":{
    "session_key":{"type":"string","minLength":1},
    "limit":{"type":"integer","minimum":1},
    "cursor":{"type":"string"}
  }
}`)
	readChatHistoryOutputSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "entries":{"type":"array"},
    "next_cursor":{"type":"string"},
    "truncated":{"type":"boolean"}
  }
}`)
	readDiaryInputSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["session_key"],
  "properties":{
    "session_key":{"type":"string","minLength":1},
    "limit":{"type":"integer","minimum":1},
    "cursor":{"type":"string"}
  }
}`)
	readDiaryOutputSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "entries":{"type":"array"},
    "next_cursor":{"type":"string"},
    "truncated":{"type":"boolean"}
  }
}`)
	writeDiaryInputSchema = json.RawMessage(`{
  "type":"object",
  "additionalProperties":false,
  "required":["session_key","content"],
  "properties":{
    "session_key":{"type":"string","minLength":1},
    "content":{"type":"string","minLength":1},
    "author":{"type":"string"},
    "tags":{"type":"array","items":{"type":"string"}},
    "metadata":{"type":"object","additionalProperties":{"type":"string"}}
  }
}`)
	writeDiaryOutputSchema = json.RawMessage(`{
  "type":"object",
  "properties":{
    "entry":{"type":"object"},
    "truncated":{"type":"boolean"}
  }
}`)
)

type readChatHistoryTool struct {
	store           chatlog.Store
	defaultListSize int
	maxListSize     int
}

func newReadChatHistoryTool(store chatlog.Store, defaultListSize, maxListSize int) Callable {
	return &readChatHistoryTool{
		store:           store,
		defaultListSize: defaultListSize,
		maxListSize:     maxListSize,
	}
}

func (t *readChatHistoryTool) Descriptor() Descriptor {
	return Descriptor{
		Name:         ToolReadChatHistory,
		Description:  "Read recent chat history from session memory.",
		Source:       SourceInternal,
		InputSchema:  readChatHistoryInputSchema,
		OutputSchema: readChatHistoryOutputSchema,
	}
}

func (t *readChatHistoryTool) Call(ctx context.Context, arguments json.RawMessage) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.store == nil {
		return errorResult(ToolReadChatHistory, ErrorCodeUnavailable, "chat store is unavailable", true), nil
	}

	var args readListArgs
	if err := decodeObject(arguments, &args); err != nil {
		return errorResult(ToolReadChatHistory, ErrorCodeInvalidArguments, err.Error(), false), nil
	}

	sessionKey, limit, cursor, err := normalizeReadListArgs(args, t.defaultListSize, t.maxListSize)
	if err != nil {
		return errorResult(ToolReadChatHistory, ErrorCodeInvalidArguments, err.Error(), false), nil
	}

	list, listErr := t.store.List(ctx, sessionKey, chatlog.ListOptions{
		Limit:  limit,
		Cursor: cursor,
	})
	if listErr != nil {
		if errors.Is(listErr, chatlog.ErrEmptySessionKey) || errors.Is(listErr, chatlog.ErrInvalidCursor) {
			return errorResult(ToolReadChatHistory, ErrorCodeInvalidArguments, listErr.Error(), false), nil
		}
		return classifyInternalStoreError(ToolReadChatHistory, listErr, chatlog.ErrInvalidCursor), nil
	}

	entries := make([]chatEntryView, 0, len(list.Entries))
	for _, entry := range list.Entries {
		entries = append(entries, toChatEntryView(entry))
	}

	structured := readListResponse[chatEntryView]{
		Entries:    entries,
		NextCursor: list.NextCursor,
	}
	structuredJSON := mustMarshalRaw(structured)

	return CallResult{
		Name:       ToolReadChatHistory,
		Content:    formatChatEntriesForPrompt(entries),
		Structured: structuredJSON,
	}, nil
}

type readDiaryTool struct {
	store           diary.Store
	defaultListSize int
	maxListSize     int
}

func newReadDiaryTool(store diary.Store, defaultListSize, maxListSize int) Callable {
	return &readDiaryTool{
		store:           store,
		defaultListSize: defaultListSize,
		maxListSize:     maxListSize,
	}
}

func (t *readDiaryTool) Descriptor() Descriptor {
	return Descriptor{
		Name:         ToolReadDiary,
		Description:  "Read diary notes from session memory.",
		Source:       SourceInternal,
		InputSchema:  readDiaryInputSchema,
		OutputSchema: readDiaryOutputSchema,
	}
}

func (t *readDiaryTool) Call(ctx context.Context, arguments json.RawMessage) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.store == nil {
		return errorResult(ToolReadDiary, ErrorCodeUnavailable, "diary store is unavailable", true), nil
	}

	var args readListArgs
	if err := decodeObject(arguments, &args); err != nil {
		return errorResult(ToolReadDiary, ErrorCodeInvalidArguments, err.Error(), false), nil
	}

	sessionKey, limit, cursor, err := normalizeReadListArgs(args, t.defaultListSize, t.maxListSize)
	if err != nil {
		return errorResult(ToolReadDiary, ErrorCodeInvalidArguments, err.Error(), false), nil
	}

	list, listErr := t.store.List(ctx, sessionKey, diary.ListOptions{
		Limit:  limit,
		Cursor: cursor,
	})
	if listErr != nil {
		if errors.Is(listErr, diary.ErrEmptySessionKey) || errors.Is(listErr, diary.ErrInvalidCursor) {
			return errorResult(ToolReadDiary, ErrorCodeInvalidArguments, listErr.Error(), false), nil
		}
		return classifyInternalStoreError(ToolReadDiary, listErr, diary.ErrInvalidCursor), nil
	}

	entries := make([]diaryEntryView, 0, len(list.Entries))
	for _, entry := range list.Entries {
		entries = append(entries, toDiaryEntryView(entry))
	}

	structured := readListResponse[diaryEntryView]{
		Entries:    entries,
		NextCursor: list.NextCursor,
	}
	structuredJSON := mustMarshalRaw(structured)

	return CallResult{
		Name:       ToolReadDiary,
		Content:    formatDiaryEntriesForPrompt(entries),
		Structured: structuredJSON,
	}, nil
}

type writeDiaryTool struct {
	store diary.Store
}

func newWriteDiaryTool(store diary.Store) Callable {
	return &writeDiaryTool{store: store}
}

func (t *writeDiaryTool) Descriptor() Descriptor {
	return Descriptor{
		Name:         ToolWriteDiary,
		Description:  "Write one diary note into session memory.",
		Source:       SourceInternal,
		InputSchema:  writeDiaryInputSchema,
		OutputSchema: writeDiaryOutputSchema,
	}
}

func (t *writeDiaryTool) Call(ctx context.Context, arguments json.RawMessage) (CallResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if t.store == nil {
		return errorResult(ToolWriteDiary, ErrorCodeUnavailable, "diary store is unavailable", true), nil
	}

	var args writeDiaryArgs
	if err := decodeObject(arguments, &args); err != nil {
		return errorResult(ToolWriteDiary, ErrorCodeInvalidArguments, err.Error(), false), nil
	}

	sessionKey := strings.TrimSpace(args.SessionKey)
	if sessionKey == "" {
		return errorResult(ToolWriteDiary, ErrorCodeInvalidArguments, "session_key is required", false), nil
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return errorResult(ToolWriteDiary, ErrorCodeInvalidArguments, "content is required", false), nil
	}

	author := strings.TrimSpace(args.Author)
	if author == "" {
		author = defaultDiaryAuthor
	}

	entry, err := t.store.Write(ctx, sessionKey, diary.Entry{
		Content:  content,
		Author:   author,
		Tags:     sanitizeTags(args.Tags),
		Metadata: sanitizeMetadata(args.Metadata),
	})
	if err != nil {
		if errors.Is(err, diary.ErrEmptySessionKey) {
			return errorResult(ToolWriteDiary, ErrorCodeInvalidArguments, err.Error(), false), nil
		}
		return classifyInternalStoreError(ToolWriteDiary, err, nil), nil
	}

	view := toDiaryEntryView(entry)
	structured := writeDiaryResponse{
		Entry: view,
	}

	return CallResult{
		Name:       ToolWriteDiary,
		Content:    fmt.Sprintf("saved diary entry id=%s", view.ID),
		Structured: mustMarshalRaw(structured),
	}, nil
}

type readListArgs struct {
	SessionKey string `json:"session_key"`
	Limit      int    `json:"limit"`
	Cursor     string `json:"cursor"`
}

type writeDiaryArgs struct {
	SessionKey string            `json:"session_key"`
	Content    string            `json:"content"`
	Author     string            `json:"author"`
	Tags       []string          `json:"tags"`
	Metadata   map[string]string `json:"metadata"`
}

type readListResponse[T any] struct {
	Entries    []T    `json:"entries"`
	NextCursor string `json:"next_cursor,omitempty"`
}

type writeDiaryResponse struct {
	Entry diaryEntryView `json:"entry"`
}

type chatEntryView struct {
	ID        string            `json:"id"`
	Role      string            `json:"role,omitempty"`
	Speaker   string            `json:"speaker,omitempty"`
	Content   string            `json:"content"`
	CreatedAt string            `json:"created_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type diaryEntryView struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Author    string            `json:"author,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	CreatedAt string            `json:"created_at,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

func decodeObject(raw json.RawMessage, out any) error {
	if out == nil {
		return errors.New("output target is required")
	}

	source := normalizeArguments(raw)
	decoder := json.NewDecoder(bytes.NewReader(source))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid arguments: %w", err)
	}

	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("invalid arguments: multiple JSON values")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("invalid arguments: %w", err)
	}
	return nil
}

func normalizeReadListArgs(args readListArgs, defaultLimit, maxLimit int) (string, int, string, error) {
	sessionKey := strings.TrimSpace(args.SessionKey)
	if sessionKey == "" {
		return "", 0, "", errors.New("session_key is required")
	}
	if args.Limit < 0 {
		return "", 0, "", errors.New("limit must be >= 0")
	}

	limit := args.Limit
	if limit == 0 {
		limit = defaultLimit
	}
	if maxLimit > 0 && limit > maxLimit {
		limit = maxLimit
	}
	cursor := strings.TrimSpace(args.Cursor)
	return sessionKey, limit, cursor, nil
}

func toChatEntryView(entry chatlog.Entry) chatEntryView {
	return chatEntryView{
		ID:        strings.TrimSpace(entry.ID),
		Role:      strings.TrimSpace(string(entry.Role)),
		Speaker:   strings.TrimSpace(entry.Speaker),
		Content:   strings.TrimSpace(entry.Content),
		CreatedAt: formatTime(entry.CreatedAt),
		Metadata:  sanitizeMetadata(entry.Metadata),
	}
}

func toDiaryEntryView(entry diary.Entry) diaryEntryView {
	return diaryEntryView{
		ID:        strings.TrimSpace(entry.ID),
		Content:   strings.TrimSpace(entry.Content),
		Author:    strings.TrimSpace(entry.Author),
		Tags:      sanitizeTags(entry.Tags),
		CreatedAt: formatTime(entry.CreatedAt),
		Metadata:  sanitizeMetadata(entry.Metadata),
	}
}

func formatChatEntriesForPrompt(entries []chatEntryView) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		attrs := make([]string, 0, 2)
		if entry.Role != "" {
			attrs = append(attrs, "role="+entry.Role)
		}
		if entry.Speaker != "" {
			attrs = append(attrs, "speaker="+entry.Speaker)
		}
		lines = append(lines, formatPromptLine(attrs, entry.Content))
	}
	return strings.Join(lines, "\n")
}

func formatDiaryEntriesForPrompt(entries []diaryEntryView) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		attrs := make([]string, 0, 2)
		if entry.Author != "" {
			attrs = append(attrs, "author="+entry.Author)
		}
		if len(entry.Tags) > 0 {
			attrs = append(attrs, "tags="+strings.Join(entry.Tags, ","))
		}
		lines = append(lines, formatPromptLine(attrs, entry.Content))
	}
	return strings.Join(lines, "\n")
}

func formatPromptLine(attrs []string, content string) string {
	content = strings.TrimSpace(content)
	if len(attrs) == 0 {
		return content
	}
	head := "[" + strings.Join(attrs, ";") + "]"
	if content == "" {
		return head
	}
	return head + " " + content
}

func sanitizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	clean := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func sanitizeMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	clean := make(map[string]string, len(metadata))
	for k, v := range metadata {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		clean[key] = strings.TrimSpace(v)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format("2006-01-02T15:04:05Z07:00")
}

func mustMarshalRaw(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		return json.RawMessage(`{"error":"marshal_failed"}`)
	}
	return data
}

func applyResultLimit(result *CallResult, maxChars int) {
	if result == nil || maxChars <= 0 {
		return
	}

	content, cutContent := truncateChars(result.Content, maxChars)
	result.Content = content

	if len(result.Structured) == 0 {
		if cutContent {
			result.Structured = markTruncated(nil, "content", maxChars)
		}
		return
	}

	if utf8.RuneCount(result.Structured) > maxChars {
		result.Structured = markTruncated(nil, "structured", maxChars)
		return
	}
	if cutContent {
		result.Structured = markTruncated(result.Structured, "content", maxChars)
	}
}

func truncateChars(value string, maxChars int) (string, bool) {
	if maxChars <= 0 || value == "" {
		return value, false
	}
	runes := []rune(value)
	if len(runes) <= maxChars {
		return value, false
	}
	if maxChars <= 3 {
		return string(runes[:maxChars]), true
	}
	trimmed := strings.TrimRightFunc(string(runes[:maxChars-3]), unicode.IsSpace)
	return trimmed + "...", true
}

func markTruncated(raw json.RawMessage, reason string, maxChars int) json.RawMessage {
	type truncatedPayload struct {
		Truncated bool   `json:"truncated"`
		Reason    string `json:"truncation_reason,omitempty"`
	}
	minimal := mustMarshalRaw(truncatedPayload{
		Truncated: true,
		Reason:    reason,
	})
	if len(raw) == 0 {
		return minimal
	}

	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return minimal
	}
	object["truncated"] = true
	if reason != "" {
		object["truncation_reason"] = reason
	}
	result := mustMarshalRaw(object)
	if maxChars > 0 && utf8.RuneCount(result) > maxChars {
		return minimal
	}
	return result
}
