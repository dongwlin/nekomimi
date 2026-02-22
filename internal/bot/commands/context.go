package commands

import (
	"fmt"
	"strings"

	zero "github.com/wdvxdr1123/ZeroBot"
)

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

func assistantLabel(ctx *zero.Ctx) string {
	if ctx == nil {
		return "name=assistant"
	}

	var name string
	var speakerID string
	if ctx.Event != nil {
		if ctx.Event.SelfTinyID != "" {
			speakerID = strings.TrimSpace(ctx.Event.SelfTinyID)
		} else if ctx.Event.SelfID != 0 {
			speakerID = fmt.Sprintf("%d", ctx.Event.SelfID)
		}
	}

	info := ctx.GetLoginInfo()
	if nick := strings.TrimSpace(info.Get("nickname").String()); nick != "" {
		name = nick
	}
	if speakerID == "" {
		if id := strings.TrimSpace(info.Get("user_id").String()); id != "" {
			speakerID = id
		}
	}

	if name == "" {
		name = "assistant"
	}
	if speakerID == "" {
		return "name=" + name
	}
	return "name=" + name + ";id=" + speakerID
}
