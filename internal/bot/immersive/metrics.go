package immersive

import (
	"github.com/dongwlin/nekomimi/internal/bot/session"
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

	if err := collector.RecordOutbound(metrics.OutboundTypeKeys(payload), messageID.ID() != 0, session.Key(ctx)); err != nil {
		log.Warn().Err(err).Msg("record immersive outbound metrics failed")
	}
	return messageID
}

func (b *ImmersiveBuffer) recordImmersiveMetrics(record metrics.ImmersiveRecord) {
	if b == nil || b.collector == nil {
		return
	}
	if err := b.collector.RecordImmersive(record); err != nil {
		log.Warn().Err(err).Msg("record immersive metrics failed")
	}
}
