package commands

import (
	"strings"
	"sync"

	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/rs/zerolog/log"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

var (
	metricsCollector        *metrics.Collector
	inboundMetricsMatcherMu sync.Once
)

func setMetricsCollector(collector *metrics.Collector) {
	metricsCollector = collector
}

func registerInboundMetricsMatchers() {
	inboundMetricsMatcherMu.Do(func() {
		handler := func(ctx *zero.Ctx) {
			recordInbound(ctx)
		}
		zero.OnMessage().Handle(handler)
		zero.OnNotice().Handle(handler)
		zero.OnRequest().Handle(handler)
		zero.OnMetaEvent().Handle(handler)
	})
}

func recordInbound(ctx *zero.Ctx) {
	collector := metricsCollector
	if collector == nil || ctx == nil || ctx.Event == nil {
		return
	}
	if err := collector.RecordInbound(ctx.Event, sessionKey(ctx)); err != nil {
		log.Warn().Err(err).Msg("record inbound metrics failed")
	}
}

func sendTracked(ctx *zero.Ctx, payload interface{}) message.ID {
	var messageID message.ID
	if ctx == nil {
		return messageID
	}

	messageID = ctx.Send(payload)
	collector := metricsCollector
	if collector == nil {
		return messageID
	}

	if err := collector.RecordOutbound(metrics.OutboundTypeKeys(payload), isSendSuccess(messageID), sessionKey(ctx)); err != nil {
		log.Warn().Err(err).Msg("record outbound metrics failed")
	}
	return messageID
}

func sendPokeTracked(ctx *zero.Ctx, groupID, userID int64) bool {
	if ctx == nil {
		return false
	}
	resp := ctx.CallAction("send_poke", zero.Params{
		"group_id": groupID,
		"user_id":  userID,
	})
	success := isPokeSuccess(resp)
	collector := metricsCollector
	if collector != nil {
		if err := collector.RecordOutbound([]string{"outbound:poke"}, success, sessionKey(ctx)); err != nil {
			log.Warn().Err(err).Msg("record poke metrics failed")
		}
	}
	return success
}

func isSendSuccess(id message.ID) bool {
	return id.ID() != 0
}

func isPokeSuccess(resp zero.APIResponse) bool {
	return strings.EqualFold(strings.TrimSpace(resp.Status), "ok") && resp.RetCode == 0
}
