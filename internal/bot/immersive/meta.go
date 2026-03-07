package immersive

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
)

// buildRecentPreview creates a formatted debug preview from the last 'keep' messages.
func buildRecentPreview(queue []queuedMessage, keep int, identity botIdentity) string {
	if len(queue) == 0 || keep <= 0 {
		return ""
	}
	start := len(queue) - keep
	if start < 0 {
		start = 0
	}
	return buildCombinedInput(queue[start:], identity)
}

// buildCombinedInput builds a complete formatted debug preview from the queue.
func buildCombinedInput(queue []queuedMessage, identity botIdentity) string {
	meta := summarizeQueueMeta(queue, time.Now(), identity)
	transcriptLines := renderTranscriptLines(queue)
	systemEventLines := renderSystemEventLines(queue)
	var builder strings.Builder
	builder.WriteString("batch_meta:\n")
	builder.WriteString("  now_date: ")
	builder.WriteString(meta.NowDate)
	builder.WriteString("\n")
	builder.WriteString("  now_time: ")
	builder.WriteString(meta.NowTime)
	builder.WriteString("\n")
	builder.WriteString("  bot_names: [")
	builder.WriteString(strings.Join(meta.BotNames, ","))
	builder.WriteString("]\n")
	builder.WriteString("  bot_primary_name: ")
	builder.WriteString(meta.BotPrimaryName)
	builder.WriteString("\n")
	builder.WriteString("  bot_config_names: [")
	builder.WriteString(strings.Join(meta.BotConfigNames, ","))
	builder.WriteString("]\n")
	builder.WriteString("  bot_account_nickname: ")
	builder.WriteString(meta.BotAccountNick)
	builder.WriteString("\n")
	builder.WriteString("  bot_account_ids: [")
	builder.WriteString(strings.Join(meta.BotAccountIDs, ","))
	builder.WriteString("]\n")
	builder.WriteString("  bot_primary_id: ")
	builder.WriteString(meta.BotPrimaryID)
	builder.WriteString("\n")
	builder.WriteString("  messages_count: ")
	builder.WriteString(strconv.Itoa(meta.MessagesCount))
	builder.WriteString("\n")
	builder.WriteString("  participants: [")
	builder.WriteString(strings.Join(meta.Participants, ","))
	builder.WriteString("]\n")
	builder.WriteString("  mentions_to_bot: ")
	builder.WriteString(strconv.Itoa(meta.MentionsToBot))
	builder.WriteString("\n")
	builder.WriteString("  addressed_to_bot: ")
	builder.WriteString(strconv.Itoa(meta.AddressedToBot))
	builder.WriteString("\n")
	builder.WriteString("  questions_count: ")
	builder.WriteString(strconv.Itoa(meta.QuestionsCount))
	builder.WriteString("\n")
	builder.WriteString("  last_speaker: ")
	builder.WriteString(meta.LastSpeaker)
	builder.WriteString("\n")
	builder.WriteString("  time_span_ms: ")
	builder.WriteString(strconv.FormatInt(meta.TimeSpanMS, 10))
	builder.WriteString("\n")
	builder.WriteString("transcript:\n")
	for _, formatted := range transcriptLines {
		builder.WriteString("  - ")
		builder.WriteString(formatted)
		builder.WriteString("\n")
	}
	if len(systemEventLines) > 0 {
		builder.WriteString("system_events:\n")
		for _, formatted := range systemEventLines {
			builder.WriteString("  - ")
			builder.WriteString(formatted)
			builder.WriteString("\n")
		}
	}
	return strings.TrimSpace(builder.String())
}

// formatQueuedMessage formats a single queued message as a transcript line
// with speaker label and optional timestamp.
func formatQueuedMessage(msg queuedMessage) string {
	if !isTranscriptEvent(msg) {
		return ""
	}
	content := strings.TrimSpace(msg.text)
	if content == "" {
		return ""
	}
	content = sanitizeInline(content)
	label := strings.TrimSpace(msg.speaker)
	timeLabel := formatMessageTime(msg.ts)
	if label == "" {
		if timeLabel == "" {
			return content
		}
		return "[time=" + timeLabel + "]: " + content
	}
	if timeLabel == "" {
		return "[" + label + "]: " + content
	}
	return "[" + label + ";time=" + timeLabel + "]: " + content
}

func renderTranscriptLines(queue []queuedMessage) []string {
	lines := make([]string, 0, len(queue))
	for _, msg := range queue {
		formatted := formatQueuedMessage(msg)
		if formatted == "" {
			continue
		}
		lines = append(lines, formatted)
	}
	return lines
}

func renderSystemEventLines(queue []queuedMessage) []string {
	lines := make([]string, 0, len(queue))
	for _, msg := range queue {
		formatted := formatSystemEventLine(msg)
		if formatted == "" {
			continue
		}
		lines = append(lines, formatted)
	}
	return lines
}

func renderSystemEventSummary(queue []queuedMessage) string {
	return strings.Join(renderSystemEventLines(queue), "\n")
}

func formatSystemEventLine(msg queuedMessage) string {
	if !isSystemEvent(msg) {
		return ""
	}
	headerParts := []string{"kind=" + string(normalizeEventKind(msg.kind))}
	if label := sanitizeInline(strings.TrimSpace(msg.speaker)); label != "" {
		headerParts = append(headerParts, "speaker="+label)
	}
	if timeLabel := formatMessageTime(msg.ts); timeLabel != "" {
		headerParts = append(headerParts, "time="+timeLabel)
	}
	header := "[" + strings.Join(headerParts, ";") + "]"
	body := formatSystemEventBody(msg)
	if body == "" {
		return header
	}
	return header + ": " + body
}

func formatSystemEventBody(msg queuedMessage) string {
	fields := make([]string, 0, len(msg.metadata)+1)
	if content := strings.TrimSpace(msg.text); content != "" {
		fields = append(fields, "text="+sanitizeInline(content))
	}
	if len(msg.metadata) > 0 {
		keys := make([]string, 0, len(msg.metadata))
		for key := range msg.metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := strings.TrimSpace(msg.metadata[key])
			if value == "" {
				continue
			}
			fields = append(fields, key+"="+sanitizeInline(value))
		}
	}
	return strings.Join(fields, " ")
}

func isTranscriptEvent(msg queuedMessage) bool {
	switch normalizeEventKind(msg.kind) {
	case EventUserMessage, EventAssistantText:
		return true
	default:
		return false
	}
}

func isSystemEvent(msg queuedMessage) bool {
	switch normalizeEventKind(msg.kind) {
	case EventPokeNotice, EventAssistantAction, EventSystemNote:
		return true
	default:
		return false
	}
}

// sanitizeInline replaces newline characters with escaped sequences for inline text.
func sanitizeInline(text string) string {
	replacer := strings.NewReplacer("\r\n", "\\n", "\n", "\\n", "\r", "\\n")
	return replacer.Replace(text)
}

// normalizedBotNames returns a sorted, deduplicated list of bot names
// with whitespace trimmed and duplicates removed.
func normalizedBotNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(names))
	result := make([]string, 0, len(names))
	for _, name := range names {
		trimmed := sanitizeInline(strings.TrimSpace(name))
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

// formatMessageTime formats a time as a readable timestamp string,
// returning empty string for zero time.
func formatMessageTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Format("2006-01-02 15:04:05")
}

// minDuration returns the smaller of two durations.
func minDuration(a, b time.Duration) time.Duration {
	if a <= b {
		return a
	}
	return b
}

// sumQueueChars calculates the total character count of all messages in the queue.
func sumQueueChars(queue []queuedMessage) int {
	total := 0
	for _, msg := range queue {
		total += msg.chars
	}
	return total
}

// prependMessages combines two message slices with head placed before tail.
func prependMessages(head, tail []queuedMessage) []queuedMessage {
	if len(head) == 0 {
		return tail
	}
	if len(tail) == 0 {
		next := make([]queuedMessage, len(head))
		copy(next, head)
		return next
	}
	next := make([]queuedMessage, 0, len(head)+len(tail))
	next = append(next, head...)
	next = append(next, tail...)
	return next
}

// appendTimelineMessage appends one message into runtime buffer with bounded length.
func appendTimelineMessage(timeline []queuedMessage, msg queuedMessage, maxMessages int) []queuedMessage {
	timeline = append(timeline, msg)
	if maxMessages <= 0 || len(timeline) <= maxMessages {
		return timeline
	}
	start := len(timeline) - maxMessages
	trimmed := make([]queuedMessage, maxMessages)
	copy(trimmed, timeline[start:])
	return trimmed
}

func trimTimelineTail(timeline []queuedMessage, maxMessages int) []queuedMessage {
	if maxMessages <= 0 || len(timeline) <= maxMessages {
		return timeline
	}
	start := len(timeline) - maxMessages
	trimmed := make([]queuedMessage, maxMessages)
	copy(trimmed, timeline[start:])
	return trimmed
}

func buildTimelineFallbackSummary(previousSummary string, messages []queuedMessage, maxChars int) string {
	parts := make([]string, 0, len(messages)+1)
	if trimmed := strings.TrimSpace(previousSummary); trimmed != "" {
		parts = append(parts, "已有摘要:"+trimmed)
	}
	for _, msg := range messages {
		content := strings.TrimSpace(msg.text)
		if content == "" {
			continue
		}
		speaker := strings.TrimSpace(msg.speaker)
		if speaker == "" {
			speaker = "unknown"
		}
		entry := speaker + ":" + strings.Join(strings.Fields(content), " ")
		parts = append(parts, entry)
	}
	if len(parts) == 0 {
		return ""
	}
	joined := strings.Join(parts, "；")
	if maxChars > 0 {
		return limitRunes(joined, maxChars)
	}
	return joined
}

func limitRunes(text string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

// buildImmersiveContext constructs an ImmersiveContext from the queue that
// carries both batch signals and session behavior state into the LLM pipeline.
func buildImmersiveContext(
	queue []queuedMessage,
	runtimeEvents []queuedMessage,
	identity botIdentity,
	behavior behaviorSnapshot,
	gate speakGateDecision,
) *contextassemble.ImmersiveContext {
	now := time.Now()
	meta := summarizeQueueMeta(queue, now, identity)
	return &contextassemble.ImmersiveContext{
		MessagesCount:          meta.MessagesCount,
		Participants:           meta.Participants,
		MentionsToBot:          meta.MentionsToBot,
		AddressedToBot:         meta.AddressedToBot,
		QuestionsCount:         meta.QuestionsCount,
		LastSpeaker:            meta.LastSpeaker,
		TimeSpanMS:             meta.TimeSpanMS,
		ConversationMode:       string(behavior.Mode),
		FocusSpeaker:           behavior.FocusSpeaker,
		EnergyValue:            behavior.EnergyValue,
		EnergyBaseline:         behavior.EnergyBaseline,
		EnergyTarget:           behavior.EnergyTarget,
		EnergyBand:             behavior.EnergyBand,
		SpeakGateOpen:          behavior.SpeakGateOpen,
		SignalScore:            gate.SignalScore,
		SignalBand:             string(gate.SignalBand),
		SignalFeatureSummary:   formatSignalFeatures(gate.SignalFeatures),
		StrongCall:             gate.StrongCall,
		LastBotReplyMS:         elapsedSinceMS(now, behavior.LastBotReplyAt),
		LastAddressedMS:        elapsedSinceMS(now, behavior.LastAddressedAt),
		PendingQuestion:        behavior.PendingQuestion,
		FollowupDueMS:          durationUntilMS(now, behavior.FollowupDueAt),
		FollowupBudget:         behavior.FollowupBudget,
		NextColdOpenEligibleMS: durationUntilMS(now, behavior.NextColdOpenEligibleAt),
		ReplyTier:              gate.ReplyTier,
		MaxReplySegments:       gate.MaxReplySegments,
		FollowupAllowed:        gate.FollowupAllowed,
		SystemEventSummary:     renderSystemEventSummary(runtimeEvents),
	}
}

func elapsedSinceMS(now, at time.Time) int64 {
	if at.IsZero() {
		return -1
	}
	if now.Before(at) {
		return 0
	}
	return now.Sub(at).Milliseconds()
}

func durationUntilMS(now, at time.Time) int64 {
	if at.IsZero() {
		return -1
	}
	return at.Sub(now).Milliseconds()
}

// summarizeQueueMeta computes aggregated metadata from a queue of messages,
// including participant count, mentions, questions, and time span.
func summarizeQueueMeta(queue []queuedMessage, now time.Time, identity botIdentity) queueMeta {
	meta := queueMeta{
		NowDate:        now.Format("2006-01-02"),
		NowTime:        now.Format("15:04:05"),
		BotNames:       []string{"bot"},
		BotPrimaryName: "bot",
		BotConfigNames: []string{"bot"},
		BotAccountNick: "unknown",
		BotAccountIDs:  []string{"unknown"},
		BotPrimaryID:   "unknown",
		MessagesCount:  0,
		LastSpeaker:    "unknown",
		Participants:   []string{"none"},
	}
	configNames := normalizedBotNames(identity.ConfigNicknames)
	if len(configNames) > 0 {
		meta.BotConfigNames = configNames
		meta.BotPrimaryName = configNames[0]
	}
	accountNick := sanitizeInline(strings.TrimSpace(identity.AccountNickname))
	if accountNick != "" {
		meta.BotAccountNick = accountNick
	}
	accountIDs := normalizedBotNames(identity.AccountIDs)
	if len(accountIDs) > 0 {
		meta.BotAccountIDs = accountIDs
		meta.BotPrimaryID = accountIDs[0]
	}
	combinedNames := append([]string{}, meta.BotConfigNames...)
	if meta.BotAccountNick != "" && meta.BotAccountNick != "unknown" {
		combinedNames = append(combinedNames, meta.BotAccountNick)
	}
	normalizedNames := normalizedBotNames(combinedNames)
	if len(normalizedNames) > 0 {
		meta.BotNames = normalizedNames
		if meta.BotPrimaryName == "" || meta.BotPrimaryName == "bot" {
			meta.BotPrimaryName = normalizedNames[0]
		}
	}
	if len(queue) == 0 {
		return meta
	}
	participants := make(map[string]struct{}, len(queue))
	var first time.Time
	var last time.Time
	var lastSpeaker string
	for _, msg := range queue {
		if !isTranscriptEvent(msg) {
			continue
		}
		meta.MessagesCount++
		label := strings.TrimSpace(msg.speaker)
		if label == "" {
			label = "unknown"
		}
		participants[label] = struct{}{}
		if msg.isMentionBot {
			meta.MentionsToBot++
		}
		if msg.isAddressedToBot {
			meta.AddressedToBot++
		}
		if msg.isQuestion {
			meta.QuestionsCount++
		}
		switch msg.nicknamePosition {
		case NickIsolated:
			meta.NicknameIsolatedCount++
		case NickStart:
			meta.NicknameStartCount++
		case NickEnd:
			meta.NicknameEndCount++
		case NickMiddle:
			meta.NicknameMiddleCount++
		}
		if msg.isQuestion && msg.isAddressedToBot {
			meta.DirectedQuestions++
		}
		if !msg.ts.IsZero() {
			if first.IsZero() || msg.ts.Before(first) {
				first = msg.ts
			}
			if last.IsZero() || msg.ts.After(last) {
				last = msg.ts
			}
		}
		lastSpeaker = label
	}
	if meta.MessagesCount == 0 {
		return meta
	}
	meta.Participants = make([]string, 0, len(participants))
	for label := range participants {
		meta.Participants = append(meta.Participants, sanitizeInline(label))
	}
	sort.Strings(meta.Participants)
	if lastSpeaker != "" {
		meta.LastSpeaker = sanitizeInline(lastSpeaker)
	}
	if !first.IsZero() && !last.IsZero() && !last.Before(first) {
		meta.TimeSpanMS = last.Sub(first).Milliseconds()
	}
	return meta
}
