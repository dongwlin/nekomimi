package immersive

import (
	"fmt"
	"strings"

	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func (b *ImmersiveBuffer) SetMetricsCollector(collector *metrics.Collector) {
	if b == nil {
		return
	}
	b.collector = collector
}

func (b *ImmersiveBuffer) sendTracked(ctx *zero.Ctx, payload interface{}) message.ID {
	var messageID message.ID
	if ctx == nil {
		return messageID
	}

	messageID = ctx.Send(payload)
	collector := b.collector
	if collector == nil {
		return messageID
	}

	if err := collector.RecordOutbound(metrics.OutboundTypeKeys(payload), messageID.ID() != 0, immersiveSessionKey(ctx)); err != nil {
		log.Warn().Err(err).Msg("record immersive outbound metrics failed")
	}
	return messageID
}

func immersiveSessionKey(ctx *zero.Ctx) string {
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
	if ctx.Event.GroupID == 0 && ctx.Event.UserID != 0 {
		return fmt.Sprintf("private:%d", ctx.Event.UserID)
	}
	if ctx.Event.GroupID == 0 {
		return "global"
	}
	return fmt.Sprintf("group:%d", ctx.Event.GroupID)
}
