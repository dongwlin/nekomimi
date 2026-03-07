package metrics

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func newTestCollector(t *testing.T) *Collector {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "auth.db")
	collector, err := NewCollector(dbPath)
	if err != nil {
		t.Fatalf("new collector failed: %v", err)
	}
	t.Cleanup(func() {
		_ = collector.Close()
	})
	return collector
}

func TestCollector_RecordAndBuildOverview(t *testing.T) {
	collector := newTestCollector(t)
	now := time.Now()
	collector.SetProcessStartedAt(now.Add(-2 * time.Hour))
	collector.SetBotStartedAt(now.Add(-90 * time.Minute))

	inboundMessage := &zero.Event{
		Time:     now.Unix(),
		PostType: "message",
		Message: message.Message{
			{Type: "text", Data: map[string]string{"text": "hello"}},
			{Type: "image", Data: map[string]string{"file": "a.png"}},
			{Type: "image", Data: map[string]string{"file": "b.png"}},
		},
	}
	if err := collector.RecordInbound(inboundMessage, "group:1"); err != nil {
		t.Fatalf("record inbound message failed: %v", err)
	}
	if err := collector.RecordInbound(&zero.Event{
		Time:       now.Unix(),
		PostType:   "notice",
		DetailType: "notify",
		SubType:    "poke",
	}, "group:1"); err != nil {
		t.Fatalf("record inbound poke notice failed: %v", err)
	}
	if err := collector.RecordInbound(&zero.Event{
		Time:       now.Unix(),
		PostType:   "request",
		DetailType: "friend",
	}, "private:42"); err != nil {
		t.Fatalf("record inbound request failed: %v", err)
	}

	if err := collector.RecordOutbound([]string{"outbound:text", "outbound:image"}, true, "group:1"); err != nil {
		t.Fatalf("record outbound success failed: %v", err)
	}
	if err := collector.RecordOutbound([]string{"outbound:text"}, false, "group:1"); err != nil {
		t.Fatalf("record outbound failure failed: %v", err)
	}

	overview, err := collector.BuildOverview(now, LLMStatus{
		Enabled:  true,
		Provider: "responses",
		Model:    "gpt-4.1-mini",
	})
	if err != nil {
		t.Fatalf("build overview failed: %v", err)
	}

	if overview.KPI.TodayReceivedTotal != 5 {
		t.Fatalf("unexpected today received total: %d", overview.KPI.TodayReceivedTotal)
	}
	if overview.KPI.TodaySentTotal != 2 {
		t.Fatalf("unexpected today sent total: %d", overview.KPI.TodaySentTotal)
	}
	if overview.KPI.TodayFailedTotal != 1 {
		t.Fatalf("unexpected today failed total: %d", overview.KPI.TodayFailedTotal)
	}
	if overview.KPI.TotalReceivedTotal != 5 || overview.KPI.TotalSentTotal != 2 || overview.KPI.TotalFailedTotal != 1 {
		t.Fatalf("unexpected total counters: %+v", overview.KPI)
	}
	if overview.KPI.TodayActiveSession != 2 {
		t.Fatalf("unexpected active session count: %d", overview.KPI.TodayActiveSession)
	}
	if !overview.KPI.LLMEnabled || overview.KPI.LLMProvider != "responses" || overview.KPI.LLMModel != "gpt-4.1-mini" {
		t.Fatalf("unexpected llm status in kpi: %+v", overview.KPI)
	}
	if overview.Runtime.BotConnectedAt == nil {
		t.Fatal("bot_connected_at should be set after inbound events")
	}
	if overview.KPI.LastReceivedAt == nil || overview.KPI.LastSentAt == nil || overview.KPI.LastFailedAt == nil {
		t.Fatalf("last timestamps should not be nil: %+v", overview.KPI)
	}
	if overview.Runtime.UptimeSeconds <= 0 || overview.Runtime.BotUptimeSeconds <= 0 {
		t.Fatalf("runtime uptimes should be positive: %+v", overview.Runtime)
	}

	if got := findTypeCount(overview.TodayInbound, "message:image"); got != 2 {
		t.Fatalf("unexpected message:image count: %d", got)
	}
	if got := findTypeCount(overview.TodayInbound, "notice:poke"); got != 1 {
		t.Fatalf("unexpected notice:poke count: %d", got)
	}
	if got := findTypeCount(overview.TodayInbound, "request:friend"); got != 1 {
		t.Fatalf("unexpected request:friend count: %d", got)
	}
	if got := findTypeCount(overview.TodayOutbound, "outbound:image"); got != 1 {
		t.Fatalf("unexpected outbound:image count: %d", got)
	}
	if got := findTypeCount(overview.TodayFailed, "outbound:text"); got != 1 {
		t.Fatalf("unexpected failed outbound:text count: %d", got)
	}

	if len(overview.HourlyTrend) != 24 {
		t.Fatalf("unexpected hourly trend length: %d", len(overview.HourlyTrend))
	}
	currentHour := now.Format("15") + ":00"
	hourPoint, ok := findHourPoint(overview.HourlyTrend, currentHour)
	if !ok {
		t.Fatalf("current hour point %q not found", currentHour)
	}
	if hourPoint.Received != 5 || hourPoint.Sent != 2 || hourPoint.Failed != 1 {
		t.Fatalf("unexpected current hour stats: %+v", hourPoint)
	}
}

func TestCollector_DailySessionDedup(t *testing.T) {
	collector := newTestCollector(t)
	now := time.Now()

	event := &zero.Event{
		Time:     now.Unix(),
		PostType: "message",
		Message: message.Message{
			{Type: "text", Data: map[string]string{"text": "hi"}},
		},
	}
	if err := collector.RecordInbound(event, "group:1"); err != nil {
		t.Fatalf("record inbound failed: %v", err)
	}
	if err := collector.RecordInbound(event, "group:1"); err != nil {
		t.Fatalf("record inbound failed: %v", err)
	}
	if err := collector.RecordOutbound([]string{"outbound:text"}, true, "group:1"); err != nil {
		t.Fatalf("record outbound failed: %v", err)
	}

	overview, err := collector.BuildOverview(now, LLMStatus{})
	if err != nil {
		t.Fatalf("build overview failed: %v", err)
	}
	if overview.KPI.TodayActiveSession != 1 {
		t.Fatalf("expected deduped active sessions=1, got %d", overview.KPI.TodayActiveSession)
	}
}

func TestCollector_RecordImmersive(t *testing.T) {
	collector := newTestCollector(t)
	if err := collector.RecordImmersive(ImmersiveRecord{
		Action:              "reply",
		ReasonCode:          "reply_action",
		SignalBand:          "high_priority",
		ProactiveKind:       "followup",
		ProactiveStatus:     "triggered",
		FastRecoveryReason:  "strong_address_fast_recovery",
		StrongCallLatencyMS: 320,
	}); err != nil {
		t.Fatalf("record immersive metrics failed: %v", err)
	}

	dayKey := time.Now().Format("2006-01-02")
	if got := findImmersiveCount(t, collector, dayKey, immersiveScopeAction, "reply"); got != 1 {
		t.Fatalf("unexpected immersive action count: %d", got)
	}
	if got := findImmersiveCount(t, collector, dayKey, immersiveScopeReason, "reply_action"); got != 1 {
		t.Fatalf("unexpected immersive reason count: %d", got)
	}
	if got := findImmersiveCount(t, collector, dayKey, immersiveScopeSignalBand, "high_priority"); got != 1 {
		t.Fatalf("unexpected immersive signal band count: %d", got)
	}
	if got := findImmersiveCount(t, collector, dayKey, immersiveScopeProactive, "followup:triggered"); got != 1 {
		t.Fatalf("unexpected immersive proactive count: %d", got)
	}
	if got := findImmersiveCount(t, collector, dayKey, immersiveScopeFastRecovery, "strong_address_fast_recovery"); got != 1 {
		t.Fatalf("unexpected immersive fast recovery count: %d", got)
	}
	count, sumMS := findImmersiveLatency(t, collector, dayKey, immersiveLatencyStrongCallReply)
	if count != 1 || sumMS != 320 {
		t.Fatalf("unexpected immersive latency aggregate: count=%d sum=%d", count, sumMS)
	}
}

func findTypeCount(items []OverviewTypeItem, typeKey string) int64 {
	for _, item := range items {
		if item.Type == typeKey {
			return item.Count
		}
	}
	return 0
}

func findHourPoint(items []OverviewHourly, hour string) (OverviewHourly, bool) {
	for _, item := range items {
		if item.Hour == hour {
			return item, true
		}
	}
	return OverviewHourly{}, false
}

func findImmersiveCount(t *testing.T, collector *Collector, dayKey, scope, value string) int64 {
	t.Helper()
	row := immersiveCounterDailyRecord{}
	if err := collector.db.NewSelect().
		Model(&row).
		Where("day_key = ? AND scope = ? AND value = ?", dayKey, scope, value).
		Limit(1).
		Scan(context.Background()); err != nil {
		t.Fatalf("load immersive counter failed: %v", err)
	}
	return row.CountTotal
}

func findImmersiveLatency(t *testing.T, collector *Collector, dayKey, metric string) (int64, int64) {
	t.Helper()
	row := immersiveLatencyDailyRecord{}
	if err := collector.db.NewSelect().
		Model(&row).
		Where("day_key = ? AND metric = ?", dayKey, metric).
		Limit(1).
		Scan(context.Background()); err != nil {
		t.Fatalf("load immersive latency failed: %v", err)
	}
	return row.CountTotal, row.SumMS
}
