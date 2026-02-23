package commands

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestIsSendSuccess(t *testing.T) {
	if isSendSuccess(message.NewMessageIDFromInteger(0)) {
		t.Fatal("message_id=0 should be treated as send failure")
	}
	if !isSendSuccess(message.NewMessageIDFromInteger(123)) {
		t.Fatal("non-zero message_id should be treated as send success")
	}
}

func TestIsPokeSuccess(t *testing.T) {
	if !isPokeSuccess(zero.APIResponse{Status: "ok", RetCode: 0}) {
		t.Fatal("status=ok and retcode=0 should be success")
	}
	if isPokeSuccess(zero.APIResponse{Status: "failed", RetCode: 0}) {
		t.Fatal("non-ok status should be failure")
	}
	if isPokeSuccess(zero.APIResponse{Status: "ok", RetCode: 100}) {
		t.Fatal("non-zero retcode should be failure")
	}
}
