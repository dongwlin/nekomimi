package contextassemble

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dongwlin/nekomimi/internal/llm/chatlog"
	"github.com/dongwlin/nekomimi/internal/llm/diary"
)

const (
	DefaultRecentChatLimit  = 50
	DefaultRecentDiaryLimit = 50
	BlockRecentChat         = "recent_chat"
	BlockRecentDiary        = "recent_diary"
	BlockMeta               = "meta"
)

var (
	ErrEmptySessionKey = errors.New("session key is required")
	ErrNilChatStore    = errors.New("chatlog store is required")
	ErrNilDiaryStore   = errors.New("diary store is required")
)

type Options struct {
	RecentChatLimit  int
	RecentDiaryLimit int
	MaxChars         int
}

type Request struct {
	SessionKey string
	Meta       Meta
	MaxChars   int
}

type Meta struct {
	Now               string
	AssistantIdentity string
	BotConfigNames    []string
	SessionType       string
}

type Block struct {
	Name      string
	Content   string
	Truncated bool
}

type Result struct {
	Blocks     []Block
	TotalChars int
}

func (r Result) Block(name string) (Block, bool) {
	for _, block := range r.Blocks {
		if block.Name == name {
			return block, true
		}
	}
	return Block{}, false
}

type Assembler struct {
	chatStore  chatlog.Store
	diaryStore diary.Store
	options    Options
}

func New(chatStore chatlog.Store, diaryStore diary.Store, opts Options) *Assembler {
	return &Assembler{
		chatStore:  chatStore,
		diaryStore: diaryStore,
		options:    opts,
	}
}

func (a *Assembler) Assemble(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if a == nil || a.chatStore == nil {
		return Result{}, ErrNilChatStore
	}
	if a.diaryStore == nil {
		return Result{}, ErrNilDiaryStore
	}

	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey == "" {
		return Result{}, ErrEmptySessionKey
	}

	chatLimit := normalizeLimit(a.options.RecentChatLimit, DefaultRecentChatLimit)
	diaryLimit := normalizeLimit(a.options.RecentDiaryLimit, DefaultRecentDiaryLimit)

	chatResult, err := a.chatStore.List(ctx, sessionKey, chatlog.ListOptions{Limit: chatLimit})
	if err != nil {
		return Result{}, fmt.Errorf("list recent chat: %w", err)
	}
	diaryResult, err := a.diaryStore.List(ctx, sessionKey, diary.ListOptions{Limit: diaryLimit})
	if err != nil {
		return Result{}, fmt.Errorf("list recent diary: %w", err)
	}

	blocks := []Block{
		{
			Name:    BlockRecentChat,
			Content: formatChatEntries(chatResult.Entries),
		},
		{
			Name:    BlockRecentDiary,
			Content: formatDiaryEntries(diaryResult.Entries),
		},
		{
			Name:    BlockMeta,
			Content: formatMeta(req.Meta),
		},
	}

	maxChars := req.MaxChars
	if maxChars <= 0 {
		maxChars = a.options.MaxChars
	}
	blocks = clipBlocks(blocks, maxChars)

	return Result{
		Blocks:     blocks,
		TotalChars: totalChars(blocks),
	}, nil
}

func normalizeLimit(v, fallback int) int {
	if v <= 0 {
		return fallback
	}
	return v
}

func formatChatEntries(entries []chatlog.Entry) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		lines = append(lines, formatChatEntry(entries[i]))
	}
	return strings.Join(lines, "\n")
}

func formatDiaryEntries(entries []diary.Entry) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		lines = append(lines, formatDiaryEntry(entries[i]))
	}
	return strings.Join(lines, "\n")
}

func formatChatEntry(entry chatlog.Entry) string {
	attrs := make([]string, 0, 2)
	if role := strings.TrimSpace(string(entry.Role)); role != "" {
		attrs = append(attrs, "role="+role)
	}
	if speaker := strings.TrimSpace(entry.Speaker); speaker != "" {
		attrs = append(attrs, "speaker="+speaker)
	}
	content := strings.TrimSpace(entry.Content)
	return formatLine(attrs, content)
}

func formatDiaryEntry(entry diary.Entry) string {
	attrs := make([]string, 0, 2)
	if author := strings.TrimSpace(entry.Author); author != "" {
		attrs = append(attrs, "author="+author)
	}
	if tags := joinTags(entry.Tags); tags != "" {
		attrs = append(attrs, "tags="+tags)
	}
	content := strings.TrimSpace(entry.Content)
	return formatLine(attrs, content)
}

func formatLine(attrs []string, content string) string {
	if len(attrs) == 0 {
		return content
	}
	header := "[" + strings.Join(attrs, ";") + "]"
	if content == "" {
		return header
	}
	return header + " " + content
}

func joinTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			filtered = append(filtered, trimmed)
		}
	}
	if len(filtered) == 0 {
		return ""
	}
	return strings.Join(filtered, ",")
}

func clipBlocks(blocks []Block, maxChars int) []Block {
	if maxChars <= 0 {
		return blocks
	}
	overflow := totalChars(blocks) - maxChars
	if overflow <= 0 {
		return blocks
	}
	order := []string{BlockMeta, BlockRecentDiary, BlockRecentChat}
	for _, name := range order {
		if overflow <= 0 {
			break
		}
		index := findBlockIndex(blocks, name)
		if index < 0 {
			continue
		}
		blockChars := charCount(blocks[index].Content)
		if blockChars == 0 {
			continue
		}
		blocks[index].Truncated = true
		if blockChars <= overflow {
			blocks[index].Content = ""
			overflow -= blockChars
			continue
		}
		keepChars := blockChars - overflow
		blocks[index].Content = truncateByChars(blocks[index].Content, keepChars)
		overflow = totalChars(blocks) - maxChars
	}
	return blocks
}

func formatMeta(meta Meta) string {
	now := strings.TrimSpace(meta.Now)
	if now == "" {
		now = "unknown"
	}

	assistantIdentity := strings.TrimSpace(meta.AssistantIdentity)
	if assistantIdentity == "" {
		assistantIdentity = "unknown"
	}

	sessionType := strings.TrimSpace(strings.ToLower(meta.SessionType))
	if sessionType == "" {
		sessionType = "unknown"
	}

	configNames := normalizeMetaNames(meta.BotConfigNames)
	if len(configNames) == 0 {
		configNames = []string{"unknown"}
	}

	var builder strings.Builder
	builder.WriteString("now=")
	builder.WriteString(now)
	builder.WriteString("\nassistant_identity=")
	builder.WriteString(assistantIdentity)
	builder.WriteString("\nbot_config_names=[")
	builder.WriteString(strings.Join(configNames, ","))
	builder.WriteString("]\nsession_type=")
	builder.WriteString(sessionType)
	return builder.String()
}

func normalizeMetaNames(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	sort.Strings(result)
	return result
}

func findBlockIndex(blocks []Block, name string) int {
	for i := range blocks {
		if blocks[i].Name == name {
			return i
		}
	}
	return -1
}

func truncateByChars(value string, keepChars int) string {
	if keepChars <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= keepChars {
		return value
	}
	return strings.TrimRightFunc(string(runes[:keepChars]), unicode.IsSpace)
}

func totalChars(blocks []Block) int {
	total := 0
	for _, block := range blocks {
		total += charCount(block.Content)
	}
	return total
}

func charCount(value string) int {
	return utf8.RuneCountInString(value)
}
