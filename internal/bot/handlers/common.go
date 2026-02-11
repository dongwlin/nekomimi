package handlers

import (
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/version"
	zero "github.com/wdvxdr1123/ZeroBot"
)

func registerBasicHandlers() {
	zero.OnFullMatch("ping").Handle(func(ctx *zero.Ctx) {
		ctx.Send("pong")
	})
	zero.OnCommand("version").Handle(func(ctx *zero.Ctx) {
		ctx.Send("当前版本: " + version.String())
	})
}

func parseActionArgs(args string) (string, string) {
	args = strings.TrimSpace(args)
	if args == "" {
		return "", ""
	}
	fields := strings.Fields(args)
	action := strings.ToLower(fields[0])
	rest := strings.TrimSpace(args[len(fields[0]):])
	return action, rest
}

func sessionKey(ctx *zero.Ctx) string {
	if ctx == nil || ctx.Event == nil {
		return "global"
	}
	if ctx.Event.DetailType == "guild" {
		userID := strings.TrimSpace(ctx.Event.TinyID)
		if userID == "" {
			userID = fmt.Sprintf("%d", ctx.Event.UserID)
		}
		return "guild:" + ctx.Event.GuildID + ":" + ctx.Event.ChannelID
	}
	if ctx.Event.DetailType == "private" {
		return fmt.Sprintf("private:%d", ctx.Event.UserID)
	}
	return fmt.Sprintf("group:%d", ctx.Event.GroupID)
}

func speakerLabel(ctx *zero.Ctx) string {
	if ctx == nil || ctx.Event == nil {
		return ""
	}
	if ctx.Event.DetailType == "private" {
		return ""
	}
	name, speakerID := speakerNameAndID(ctx)
	if strings.TrimSpace(name) == "" {
		if strings.TrimSpace(speakerID) == "" {
			return "user"
		}
		return "id=" + speakerID
	}
	if strings.TrimSpace(speakerID) == "" {
		return "name=" + name
	}
	return "name=" + name + ";id=" + speakerID
}

func speakerNameAndID(ctx *zero.Ctx) (string, string) {
	if ctx == nil || ctx.Event == nil {
		return "", ""
	}
	var name string
	var speakerID string
	if ctx.Event.Sender != nil {
		name = strings.TrimSpace(ctx.Event.Sender.AnonymousName)
		if name == "" {
			name = strings.TrimSpace(ctx.Event.Sender.Card)
		}
		if name == "" {
			name = strings.TrimSpace(ctx.Event.Sender.NickName)
		}
		if ctx.Event.Sender.TinyID != "" {
			speakerID = strings.TrimSpace(ctx.Event.Sender.TinyID)
		} else if ctx.Event.Sender.ID != 0 {
			speakerID = fmt.Sprintf("%d", ctx.Event.Sender.ID)
		}
	}
	if speakerID == "" {
		speakerID = strings.TrimSpace(ctx.Event.TinyID)
	}
	if speakerID == "" && ctx.Event.UserID != 0 {
		speakerID = fmt.Sprintf("%d", ctx.Event.UserID)
	}
	return name, speakerID
}
