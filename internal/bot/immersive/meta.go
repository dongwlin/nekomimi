package immersive

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// trimRecent removes samples from the recent activity list that are older than
// the specified time window.
func trimRecent(recent []recentSample, now time.Time, windowMS int) []recentSample {
	if len(recent) == 0 {
		return recent
	}
	cutoff := now.Add(-time.Duration(windowMS) * time.Millisecond)
	start := 0
	for start < len(recent) && recent[start].ts.Before(cutoff) {
		start++
	}
	if start == 0 {
		return recent
	}
	trimmed := make([]recentSample, len(recent)-start)
	copy(trimmed, recent[start:])
	return trimmed
}

// summarizeRecent returns the count of recent samples and total character count.
func summarizeRecent(recent []recentSample) (int, int) {
	count := len(recent)
	totalChars := 0
	for _, sample := range recent {
		totalChars += sample.chars
	}
	return count, totalChars
}

// buildRecentPreview creates a formatted input string from the last 'keep' messages
// in the queue for use in LLM prompts.
func buildRecentPreview(queue []queuedMessage, keep int) string {
	if len(queue) == 0 || keep <= 0 {
		return ""
	}
	start := len(queue) - keep
	if start < 0 {
		start = 0
	}
	return buildCombinedInput(queue[start:])
}

// buildCombinedInput builds a complete formatted input string from the queue,
// including metadata about the batch and all messages in transcript format.
func buildCombinedInput(queue []queuedMessage) string {
	meta := summarizeQueueMeta(queue, time.Now(), nil)
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
	for _, msg := range queue {
		formatted := formatQueuedMessage(msg)
		if formatted == "" {
			continue
		}
		builder.WriteString("  - ")
		builder.WriteString(formatted)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func buildCombinedInputWithSummary(queue []queuedMessage, summary string) string {
	base := buildCombinedInput(queue)
	trimmedSummary := strings.TrimSpace(summary)
	if trimmedSummary == "" {
		return base
	}
	var builder strings.Builder
	builder.WriteString("history_summary: ")
	builder.WriteString(sanitizeInline(trimmedSummary))
	if strings.TrimSpace(base) != "" {
		builder.WriteString("\n")
		builder.WriteString(base)
	}
	return strings.TrimSpace(builder.String())
}

// formatQueuedMessage formats a single queued message as a transcript line
// with speaker label and optional timestamp.
func formatQueuedMessage(msg queuedMessage) string {
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

// appendTimelineMessage appends one message into timeline with bounded length.
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

// summarizeQueueMeta computes aggregated metadata from a queue of messages,
// including participant count, mentions, questions, and time span.
func summarizeQueueMeta(queue []queuedMessage, now time.Time, botNames []string) queueMeta {
	meta := queueMeta{
		NowDate:        now.Format("2006-01-02"),
		NowTime:        now.Format("15:04:05"),
		BotNames:       []string{"bot"},
		BotPrimaryName: "bot",
		MessagesCount:  len(queue),
		LastSpeaker:    "unknown",
		Participants:   []string{"none"},
	}
	normalizedNames := normalizedBotNames(botNames)
	if len(normalizedNames) > 0 {
		meta.BotNames = normalizedNames
		meta.BotPrimaryName = normalizedNames[0]
	}
	if len(queue) == 0 {
		return meta
	}
	participants := make(map[string]struct{}, len(queue))
	first := queue[0].ts
	last := queue[len(queue)-1].ts
	for _, msg := range queue {
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
		if !msg.ts.IsZero() {
			if first.IsZero() || msg.ts.Before(first) {
				first = msg.ts
			}
			if last.IsZero() || msg.ts.After(last) {
				last = msg.ts
			}
		}
	}
	meta.Participants = make([]string, 0, len(participants))
	for label := range participants {
		meta.Participants = append(meta.Participants, sanitizeInline(label))
	}
	sort.Strings(meta.Participants)
	lastSpeaker := strings.TrimSpace(queue[len(queue)-1].speaker)
	if lastSpeaker != "" {
		meta.LastSpeaker = sanitizeInline(lastSpeaker)
	}
	if !first.IsZero() && !last.IsZero() && !last.Before(first) {
		meta.TimeSpanMS = last.Sub(first).Milliseconds()
	}
	return meta
}
