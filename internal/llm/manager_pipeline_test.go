package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/llm/contextassemble"
)

func TestBuildPipelineMessages_NilImmersiveContext_NoSignalsBlock(t *testing.T) {
	manager := newMinimalManagerForPipelineTest(t)

	messages, _, err := manager.buildPipelineMessages(
		context.Background(),
		nil, "",
		contextassemble.Meta{},
		"hello world",
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content
	for _, blockName := range []string{
		contextassemble.BlockImmersiveState,
		contextassemble.BlockImmersiveBatch,
		contextassemble.BlockImmersiveSignals,
	} {
		if strings.Contains(content, blockName) {
			t.Fatalf("%s block should not be present when ImmersiveContext is nil", blockName)
		}
	}
}

func TestBuildPipelineMessages_WithImmersiveContext_UsesImmersiveBlocksWithoutPersistentContext(t *testing.T) {
	manager := newMinimalManagerForPipelineTest(t)

	ic := &contextassemble.ImmersiveContext{
		MessagesCount:    5,
		Participants:     []string{"alice", "bob"},
		MentionsToBot:    2,
		AddressedToBot:   1,
		QuestionsCount:   3,
		LastSpeaker:      "bob",
		TimeSpanMS:       4200,
		ConversationMode: "addressed",
		EnergyValue:      61,
	}

	messages, _, err := manager.buildPipelineMessages(
		context.Background(),
		nil, "",
		contextassemble.Meta{},
		"hello world",
		ic,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content
	if !strings.Contains(content, "hello world") {
		t.Fatal("expected fallback content to be present")
	}
	if !strings.Contains(content, "[immersive_batch]") {
		t.Fatalf("immersive_batch block missing from content:\n%s", content)
	}
	if !strings.Contains(content, "conversation_mode: addressed") {
		t.Fatalf("immersive_state block missing from content:\n%s", content)
	}
}

func TestBuildPipelineMessages_WithAssembler_ImmersiveContextAppended(t *testing.T) {
	manager := newMinimalManagerForPipelineTest(t)

	sessionKey := "test-session-ic"
	manager.AppendTurn(sessionKey, "neko current batch", "alice", "ok")

	ic := &contextassemble.ImmersiveContext{
		MessagesCount:    3,
		Participants:     []string{"alice"},
		MentionsToBot:    1,
		AddressedToBot:   0,
		QuestionsCount:   1,
		LastSpeaker:      "alice",
		TimeSpanMS:       1000,
		ConversationMode: "addressed",
		SignalScore:      8,
	}

	state := manager.snapshotPipelineState()
	messages, _, err := manager.buildPipelineMessages(
		context.Background(),
		state.assembler,
		sessionKey,
		contextassemble.Meta{},
		"fallback content",
		ic,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content

	for _, blockName := range []string{
		contextassemble.BlockImmersiveState,
		contextassemble.BlockImmersiveBatch,
		contextassemble.BlockImmersiveSignals,
	} {
		if !strings.Contains(content, "["+blockName+"]") {
			t.Fatalf("%s block missing from assembled content:\n%s", blockName, content)
		}
	}
	if !strings.Contains(content, "mentions_to_bot: 1") {
		t.Fatalf("mentions_to_bot signal missing from content:\n%s", content)
	}
	if !strings.Contains(content, "questions_count: 1") {
		t.Fatalf("questions_count signal missing from content:\n%s", content)
	}
	if strings.Count(content, "neko current batch") != 1 {
		t.Fatalf("expected current batch to appear exactly once in assembled content:\n%s", content)
	}
	if strings.Contains(content, "fallback content") {
		t.Fatalf("fallback content should not be included when assembled blocks already exist:\n%s", content)
	}
}

func TestBuildPipelineMessages_WithAssembler_NilImmersiveContext_NoSignals(t *testing.T) {
	manager := newMinimalManagerForPipelineTest(t)

	sessionKey := "test-session-no-ic"
	manager.AppendTurn(sessionKey, "existing message", "alice", "ok")

	state := manager.snapshotPipelineState()
	messages, _, err := manager.buildPipelineMessages(
		context.Background(),
		state.assembler,
		sessionKey,
		contextassemble.Meta{},
		"fallback content",
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content
	for _, blockName := range []string{
		contextassemble.BlockImmersiveState,
		contextassemble.BlockImmersiveBatch,
		contextassemble.BlockImmersiveSignals,
	} {
		if strings.Contains(content, "["+blockName+"]") {
			t.Fatalf("%s block should not be present when ImmersiveContext is nil:\n%s", blockName, content)
		}
	}
}

func newMinimalManagerForPipelineTest(t *testing.T) *Manager {
	t.Helper()
	return NewManager(defaultTestLLMConfig(), ManagerDeps{})
}

func defaultTestLLMConfig() config.LLMConfig {
	return config.LLMConfig{
		Enabled:  true,
		Provider: "responses",
		Model:    "test-model",
		API:      "http://localhost/responses",
		Key:      "test-key",
	}
}
