package session

import (
	"fmt"

	zero "github.com/wdvxdr1123/ZeroBot"
)

// Key builds a deterministic session identifier from a ZeroBot event context.
// The returned key has the form "group:<id>", "private:<id>",
// "guild:<guildID>:<channelID>", or "global" as a fallback.
func Key(ctx *zero.Ctx) string {
	if ctx == nil || ctx.Event == nil {
		return "global"
	}
	if ctx.Event.DetailType == "guild" {
		return "guild:" + ctx.Event.GuildID + ":" + ctx.Event.ChannelID
	}
	if ctx.Event.DetailType == "private" {
		return fmt.Sprintf("private:%d", ctx.Event.UserID)
	}
	if ctx.Event.GroupID == 0 && ctx.Event.UserID != 0 {
		return fmt.Sprintf("private:%d", ctx.Event.UserID)
	}
	if ctx.Event.GroupID == 0 {
		return "global"
	}
	return fmt.Sprintf("group:%d", ctx.Event.GroupID)
}
