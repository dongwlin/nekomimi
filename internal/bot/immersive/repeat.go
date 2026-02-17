package immersive

import "strings"

// detectConsecutiveRepeat finds the latest consecutive repeated phrase in queue.
// A valid repeat requires:
// - same normalized text in consecutive messages
// - at least 2 messages in that consecutive run
// - at least 2 distinct speakers in that consecutive run
func detectConsecutiveRepeat(queue []queuedMessage) (text string, repeatCount int, participants int) {
	if len(queue) < 2 {
		return "", 0, 0
	}

	bestText := ""
	bestRepeatCount := 0
	bestParticipants := 0

	for i := 0; i < len(queue); {
		base := normalizeRepeatText(queue[i].text)
		j := i + 1
		for j < len(queue) && base != "" && normalizeRepeatText(queue[j].text) == base {
			j++
		}
		runCount := j - i
		if base != "" && runCount >= 2 {
			speakers := make(map[string]struct{}, runCount)
			for _, msg := range queue[i:j] {
				speaker := strings.TrimSpace(msg.speaker)
				if speaker == "" {
					continue
				}
				speakers[speaker] = struct{}{}
			}
			if len(speakers) >= 2 {
				bestText = strings.TrimSpace(queue[j-1].text)
				bestRepeatCount = runCount
				bestParticipants = len(speakers)
			}
		}
		if j == i+1 {
			i++
			continue
		}
		i = j
	}

	return bestText, bestRepeatCount, bestParticipants
}

func normalizeRepeatText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return strings.Join(strings.Fields(trimmed), " ")
}
