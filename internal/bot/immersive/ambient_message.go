package immersive

import (
	"strconv"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/llm"
	zero "github.com/wdvxdr1123/ZeroBot"
)

// AmbientMessageMeta captures one inbound ambient message after router parsing.
type AmbientMessageMeta struct {
	Text             string
	Speaker          string
	At               time.Time
	IsPrivate        bool
	MentionBot       bool
	AddressedToBot   bool
	Question         bool
	DirectedQuestion bool
	NicknamePosition NicknamePosition
}

func NewAmbientMessageMeta(text, speaker string, isPrivate bool, at time.Time) AmbientMessageMeta {
	trimmed := strings.TrimSpace(text)
	if at.IsZero() {
		at = time.Now()
	}
	return AmbientMessageMeta{
		Text:      trimmed,
		Speaker:   strings.TrimSpace(speaker),
		At:        at,
		IsPrivate: isPrivate,
		Question:  looksLikeQuestion(trimmed),
	}
}

func (m AmbientMessageMeta) HistoryMetadata() map[string]string {
	return map[string]string{
		llm.MetadataRawText:          strings.TrimSpace(m.Text),
		llm.MetadataSpeakerLabel:     strings.TrimSpace(m.Speaker),
		llm.MetadataIsPrivate:        strconv.FormatBool(m.IsPrivate),
		llm.MetadataMentionBot:       strconv.FormatBool(m.MentionBot),
		llm.MetadataAddressedToBot:   strconv.FormatBool(m.AddressedToBot),
		llm.MetadataQuestion:         strconv.FormatBool(m.Question),
		llm.MetadataDirectedQuestion: strconv.FormatBool(m.DirectedQuestion),
		llm.MetadataNicknamePosition: strconv.Itoa(int(m.NicknamePosition)),
	}
}

func (m AmbientMessageMeta) HasStrongAddress() bool {
	return m.MentionBot || m.NicknamePosition >= NickStart
}

func (b *ImmersiveBuffer) AnalyzeAmbientMessage(ctx *zero.Ctx, text, speaker string, isPrivate bool, at time.Time) AmbientMessageMeta {
	meta := NewAmbientMessageMeta(text, speaker, isPrivate, at)
	if b == nil {
		return meta
	}
	mention, addressed, question, nickPos := b.detectMessageSignals(ctx, meta.Text)
	meta.MentionBot = mention
	meta.AddressedToBot = addressed
	meta.Question = question
	meta.DirectedQuestion = question && addressed
	meta.NicknamePosition = nickPos
	return meta
}

func (b *ImmersiveBuffer) ShouldYieldToImmersive(sessionKey string, meta AmbientMessageMeta) bool {
	if b == nil {
		return false
	}
	if meta.IsPrivate {
		return true
	}
	if meta.MentionBot || meta.DirectedQuestion || meta.NicknamePosition >= NickStart {
		return true
	}

	state := b.lookupSession(sessionKey)
	if state == nil {
		return false
	}

	now := meta.At
	if now.IsZero() {
		now = time.Now()
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureBehaviorDefaultsLocked(now)
	state.settleEnergyLocked(now, "lazy_router_recovery")
	state.decayBehaviorLocked(now)

	if !sameSpeaker(meta.Speaker, state.focusSpeaker) {
		return false
	}
	switch state.mode {
	case ModeAddressed, ModeInThread, ModeWaitingUser:
		return true
	default:
		return false
	}
}
