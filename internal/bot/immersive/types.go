// Package immersive provides message buffering and response management for immersive bot conversations.
// It implements intelligent message queuing with immediate flush scheduling, speak gating,
// and LLM-based decision making for when the bot should respond.
package immersive

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	"github.com/dongwlin/nekomimi/internal/metrics"
)

// ImmersiveBuffer manages message buffering and response timing for a bot session.
// It collects messages into batches and determines when to trigger a response based on
// speak gates and LLM-based judgments.
type ImmersiveBuffer struct {
	cfg       config.ImmersiveConfig
	llm       llm.Service
	collector *metrics.Collector
	nicknames []string
	identity  botIdentity
	mu        sync.Mutex
	sessions  map[string]*immersiveSession
}

// SendFunc captures how to deliver a payload for the current session.
// It is created while the incoming context is still valid and reused by async flushes.
type SendFunc func(payload interface{})

type botIdentity struct {
	ConfigNicknames []string
	AccountNickname string
	AccountIDs      []string
}

// EventKind classifies runtime events stored in the immersive buffer.
type EventKind string

const (
	EventUserMessage     EventKind = "user_message"
	EventAssistantText   EventKind = "assistant_text"
	EventPokeNotice      EventKind = "poke_notice"
	EventAssistantAction EventKind = "assistant_action"
	EventRepeatTrigger   EventKind = "repeat_trigger"
	EventSystemNote      EventKind = "system_note"
)

const (
	eventMetaAction             = "action"
	eventMetaDirection          = "direction"
	eventMetaActorName          = "actor_name"
	eventMetaTargetName         = "target_name"
	eventMetaRepeatCount        = "repeat_count"
	eventMetaRepeatParticipants = "repeat_participants"
)

// TimelineEvent is the external typed event shape used by commands and tests.
type TimelineEvent struct {
	Kind     EventKind
	Text     string
	Speaker  string
	At       time.Time
	Metadata map[string]string
}

// FlushDecision represents the adaptive scheduling result for a message batch.
type FlushDecision struct {
	Delay    time.Duration
	Reason   string
	Priority string // "immediate", "fast", "normal", "deferred"
}

// immersiveSession holds the runtime scheduling buffer for one conversation session.
// nextBatch/processingBatch are short-lived flush buffers, while runtimeBuffer is an
// in-memory prompt aid and not a durable history source.
type immersiveSession struct {
	mu              sync.Mutex
	nextBatch       []queuedMessage
	nextBatchChars  int
	batchStartTime  time.Time
	processingBatch []queuedMessage
	runtimeBuffer   []queuedMessage
	timer           *time.Timer
	inFlight        bool
	sendFn          SendFunc
	waitRounds      int

	mode                   ConversationMode
	focusSpeaker           string
	lastBotReplyAt         time.Time
	lastAddressedAt        time.Time
	lastTransitionReason   string
	energy                 float64
	energyBaseline         float64
	energyTarget           float64
	lastEnergyUpdateAt     time.Time
	energyLastDeltaReason  string
	speakGateOpen          bool
	pendingQuestion        bool
	followupDueAt          time.Time
	followupBudget         int
	followupTimer          *time.Timer
	nextColdOpenEligibleAt time.Time
	lastMessageAt          time.Time
	coldOpenEligible       bool
	coldOpenActivityCount  int
}

// queuedMessage represents a single message in the buffer queue.
type queuedMessage struct {
	kind             EventKind
	text             string
	speaker          string
	ts               time.Time
	chars            int
	metadata         map[string]string
	persisted        bool
	causalSeq        int64
	isMentionBot     bool
	isQuestion       bool
	isAddressedToBot bool
	nicknamePosition NicknamePosition
}

// queueMeta contains aggregated metadata about the message queue.
type queueMeta struct {
	NowDate        string
	NowTime        string
	BotNames       []string
	BotPrimaryName string
	BotConfigNames []string
	BotAccountNick string
	BotAccountIDs  []string
	BotPrimaryID   string
	MessagesCount  int
	Participants   []string
	MentionsToBot  int
	AddressedToBot int
	QuestionsCount int
	LastSpeaker    string
	TimeSpanMS     int64

	NicknameIsolatedCount int
	NicknameStartCount    int
	NicknameEndCount      int
	NicknameMiddleCount   int
	DirectedQuestions     int
}

func normalizeEventKind(kind EventKind) EventKind {
	switch kind {
	case EventAssistantText, EventPokeNotice, EventAssistantAction, EventRepeatTrigger, EventSystemNote:
		return kind
	case EventUserMessage:
		return kind
	default:
		return EventUserMessage
	}
}

func cloneEventMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		cloned[trimmedKey] = strings.TrimSpace(value)
	}
	if len(cloned) == 0 {
		return nil
	}
	return cloned
}

func newQueuedMessage(kind EventKind, text, speaker string, at time.Time, metadata map[string]string) queuedMessage {
	trimmedText := strings.TrimSpace(text)
	if at.IsZero() {
		at = time.Now()
	}
	return queuedMessage{
		kind:     normalizeEventKind(kind),
		text:     trimmedText,
		speaker:  strings.TrimSpace(speaker),
		ts:       at,
		chars:    len([]rune(trimmedText)),
		metadata: cloneEventMetadata(metadata),
	}
}

func queuedMessageFromTimelineEvent(event TimelineEvent) queuedMessage {
	return newQueuedMessage(event.Kind, event.Text, event.Speaker, event.At, event.Metadata)
}

func NewTimelineEvent(kind EventKind, text, speaker string, at time.Time, metadata map[string]string) TimelineEvent {
	return TimelineEvent{
		Kind:     normalizeEventKind(kind),
		Text:     strings.TrimSpace(text),
		Speaker:  strings.TrimSpace(speaker),
		At:       at,
		Metadata: cloneEventMetadata(metadata),
	}
}

func NewAssistantTextEvent(text, speaker string, at time.Time) TimelineEvent {
	return NewTimelineEvent(EventAssistantText, text, speaker, at, nil)
}

func NewPokeNoticeEvent(speaker, actorName, direction string, at time.Time) TimelineEvent {
	return NewTimelineEvent(EventPokeNotice, "", speaker, at, map[string]string{
		eventMetaActorName: actorName,
		eventMetaDirection: direction,
	})
}

func NewAssistantActionEvent(speaker, action string, at time.Time, metadata map[string]string) TimelineEvent {
	next := cloneEventMetadata(metadata)
	if next == nil {
		next = make(map[string]string, 1)
	}
	next[eventMetaAction] = strings.TrimSpace(action)
	return NewTimelineEvent(EventAssistantAction, "", speaker, at, next)
}

func NewRepeatTriggerEvent(text, speaker string, repeatCount, participants int, at time.Time) TimelineEvent {
	return NewTimelineEvent(EventRepeatTrigger, text, speaker, at, map[string]string{
		eventMetaRepeatCount:        strconv.Itoa(repeatCount),
		eventMetaRepeatParticipants: strconv.Itoa(participants),
	})
}

func NewSystemNoteEvent(text, speaker string, at time.Time) TimelineEvent {
	return NewTimelineEvent(EventSystemNote, text, speaker, at, nil)
}
