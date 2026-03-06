package session

import (
	"testing"

	zero "github.com/wdvxdr1123/ZeroBot"
)

func TestKey(t *testing.T) {
	tests := []struct {
		name string
		ctx  *zero.Ctx
		want string
	}{
		{
			name: "nil ctx",
			ctx:  nil,
			want: "global",
		},
		{
			name: "nil event",
			ctx:  &zero.Ctx{},
			want: "global",
		},
		{
			name: "group message",
			ctx: &zero.Ctx{Event: &zero.Event{
				GroupID: 12345,
			}},
			want: "group:12345",
		},
		{
			name: "private by detail type",
			ctx: &zero.Ctx{Event: &zero.Event{
				DetailType: "private",
				UserID:     99,
			}},
			want: "private:99",
		},
		{
			name: "private by zero group id",
			ctx: &zero.Ctx{Event: &zero.Event{
				GroupID: 0,
				UserID:  42,
			}},
			want: "private:42",
		},
		{
			name: "guild message",
			ctx: &zero.Ctx{Event: &zero.Event{
				DetailType: "guild",
				GuildID:    "g100",
				ChannelID:  "c200",
			}},
			want: "guild:g100:c200",
		},
		{
			name: "zero group and zero user falls back to global",
			ctx: &zero.Ctx{Event: &zero.Event{
				GroupID: 0,
				UserID:  0,
			}},
			want: "global",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Key(tt.ctx)
			if got != tt.want {
				t.Errorf("Key() = %q, want %q", got, tt.want)
			}
		})
	}
}
