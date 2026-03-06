// Package immersive provides message buffering and response management for immersive bot conversations.
// It implements intelligent message queuing with immediate flush scheduling, speak gating,
// and LLM-based decision making for when the bot should respond.
package immersive

import (
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
	nextColdOpenEligibleAt time.Time
}

// queuedMessage represents a single message in the buffer queue.
type queuedMessage struct {
	text             string
	speaker          string
	ts               time.Time
	chars            int
	persisted        bool
	causalSeq        int64
	isMentionBot     bool
	isQuestion       bool
	isAddressedToBot bool
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
}
