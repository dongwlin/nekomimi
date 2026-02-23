package metrics

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func TestInboundTypeKeys_MessageCountsSegments(t *testing.T) {
	event := &zero.Event{
		PostType: "message",
		Message: message.Message{
			{Type: "text", Data: map[string]string{"text": "hello"}},
			{Type: "image", Data: map[string]string{"file": "a.png"}},
			{Type: "image", Data: map[string]string{"file": "b.png"}},
			{Type: "reply", Data: map[string]string{"id": "1"}},
		},
	}

	got := InboundTypeKeys(event)
	want := []string{"message:text", "message:image", "message:image", "message:reply"}
	if len(got) != len(want) {
		t.Fatalf("unexpected type count: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected type at %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestInboundTypeKeys_NoticeRequestMeta(t *testing.T) {
	tests := []struct {
		name  string
		event *zero.Event
		want  string
	}{
		{
			name: "notice poke",
			event: &zero.Event{
				PostType:   "notice",
				DetailType: "notify",
				SubType:    "poke",
			},
			want: "notice:poke",
		},
		{
			name: "notice other",
			event: &zero.Event{
				PostType:   "notice",
				DetailType: "group_upload",
			},
			want: "notice:other",
		},
		{
			name: "request friend",
			event: &zero.Event{
				PostType:   "request",
				DetailType: "friend",
			},
			want: "request:friend",
		},
		{
			name: "request group",
			event: &zero.Event{
				PostType:   "request",
				DetailType: "group",
			},
			want: "request:group",
		},
		{
			name: "meta event",
			event: &zero.Event{
				PostType: "meta_event",
			},
			want: "meta_event:other",
		},
	}

	for _, tt := range tests {
		got := InboundTypeKeys(tt.event)
		if len(got) != 1 || got[0] != tt.want {
			t.Fatalf("%s unexpected result: %#v", tt.name, got)
		}
	}
}

func TestOutboundTypeKeys_MultiLabel(t *testing.T) {
	payload := message.Message{
		{Type: "text", Data: map[string]string{"text": "hello"}},
		{Type: "image", Data: map[string]string{"file": "a.png"}},
		{Type: "image", Data: map[string]string{"file": "b.png"}},
		{Type: "face", Data: map[string]string{"id": "14"}},
	}
	got := OutboundTypeKeys(payload)
	want := []string{"outbound:text", "outbound:image", "outbound:image", "outbound:other"}
	if len(got) != len(want) {
		t.Fatalf("unexpected outbound type count: got=%d want=%d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected outbound type at %d: got=%q want=%q", i, got[i], want[i])
		}
	}
}
