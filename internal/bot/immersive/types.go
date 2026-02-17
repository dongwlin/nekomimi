// Package immersive provides message buffering and cooldown management for immersive bot conversations.
// It implements intelligent message queuing with configurable cooling periods, speak gating,
// and LLM-based decision making for when the bot should respond.
package immersive

import (
	"context"
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
	identity  botIdentity
	mu        sync.Mutex
	sessions  map[string]*immersiveSession
}

type botIdentity struct {
	ConfigNicknames []string
	AccountNickname string
	AccountIDs      []string
}

// immersiveSession holds the state for a single conversation session.
type immersiveSession struct {
	mu              sync.Mutex
	queue           []queuedMessage
	queueChars      int
	timeline        []queuedMessage
	timelineSummary string
	recent          []recentSample
	timer           *time.Timer
	inFlight        bool
	lastCtx         *zero.Ctx
	waitRounds      int
	pregen          preGenerateState
}

// preGenerateState tracks the lifecycle of a session pre-generated reply.
type preGenerateState struct {
	version     uint64
	input       string
	reply       string
	err         error
	done        chan struct{}
	cancel      context.CancelFunc
	running     bool
	regenCount  int
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

// speakGateResult contains the decision result from the speak gate evaluation.
type speakGateResult struct {
	shouldSpeak       bool
	waitMS            int
	reason            string
	assistantStatus   string
	mentionsToBot     int
	addressedToBot    int
	questionsCount    int
	directedQuestions int
	messagesCount     int
	participantsCount int
}
