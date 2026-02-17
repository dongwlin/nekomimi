// Package buffer provides message buffering and cooldown management for immersive bot conversations.
// It implements intelligent message queuing with configurable cooling periods, speak gating,
// and LLM-based decision making for when the bot should respond.
package buffer

import (
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// ImmersiveBuffer manages message buffering and response timing for a bot session.
// It collects messages into batches and determines when to trigger a response based on
// cooldown calculations, speak gates, and LLM-based judgments.
type ImmersiveBuffer struct {
	cfg       config.ImmersiveConfig
	llm       *llm.Manager
	nicknames []string
	mu        sync.Mutex
	sessions  map[string]*immersiveSession
}

// immersiveSession holds the state for a single conversation session.
type immersiveSession struct {
	mu         sync.Mutex
	queue      []queuedMessage
	queueChars int
	recent     []recentSample
	timer      *time.Timer
	inFlight   bool
	lastCtx    *zero.Ctx
	postRounds int
}

// queuedMessage represents a single message in the buffer queue.
type queuedMessage struct {
	text             string
	speaker          string
	ts               time.Time
	chars            int
	isMentionBot     bool
	isQuestion       bool
	isAddressedToBot bool
}

// recentSample tracks recent message activity for cooldown calculation.
type recentSample struct {
	ts    time.Time
	chars int
}

// queueMeta contains aggregated metadata about the message queue.
type queueMeta struct {
	NowDate        string
	NowTime        string
	BotNames       []string
	BotPrimaryName string
	MessagesCount  int
	Participants   []string
	MentionsToBot  int
	AddressedToBot int
	QuestionsCount int
	LastSpeaker    string
	TimeSpanMS     int64
}

// speakGateResult contains the decision result from the speak gate evaluation.
type speakGateResult struct {
	shouldSpeak       bool
	reason            string
	assistantStatus   string
	mentionsToBot     int
	addressedToBot    int
	questionsCount    int
	directedQuestions int
	messagesCount     int
	participantsCount int
}
