package buffer

import (
	"sync"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

type ImmersiveBuffer struct {
	cfg       config.ImmersiveConfig
	llm       *llm.Manager
	nicknames []string
	mu        sync.Mutex
	sessions  map[string]*immersiveSession
}

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

type queuedMessage struct {
	text             string
	speaker          string
	ts               time.Time
	chars            int
	isMentionBot     bool
	isQuestion       bool
	isAddressedToBot bool
}

type recentSample struct {
	ts    time.Time
	chars int
}

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
