package metrics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/uptrace/bun"
)

const (
	immersiveScopeAction       = "action"
	immersiveScopeReason       = "reason"
	immersiveScopeSignalBand   = "signal_band"
	immersiveScopeProactive    = "proactive"
	immersiveScopeFastRecovery = "fast_recovery"

	immersiveLatencyStrongCallReply = "strong_call_reply"
)

// ImmersiveRecord records one compact immersive observability event.
type ImmersiveRecord struct {
	Action              string
	ReasonCode          string
	SignalBand          string
	ProactiveKind       string
	ProactiveStatus     string
	FastRecoveryReason  string
	StrongCallLatencyMS int64
}

type immersiveCounterDailyRecord struct {
	bun.BaseModel `bun:"table:metrics_immersive_daily,alias:metrics_immersive_daily"`

	DayKey     string `bun:"day_key,pk,type:text"`
	Scope      string `bun:"scope,pk,type:text"`
	Value      string `bun:"value,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
}

type immersiveCounterTotalRecord struct {
	bun.BaseModel `bun:"table:metrics_immersive_total,alias:metrics_immersive_total"`

	Scope      string `bun:"scope,pk,type:text"`
	Value      string `bun:"value,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
}

type immersiveLatencyDailyRecord struct {
	bun.BaseModel `bun:"table:metrics_immersive_latency_daily,alias:metrics_immersive_latency_daily"`

	DayKey     string `bun:"day_key,pk,type:text"`
	Metric     string `bun:"metric,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
	SumMS      int64  `bun:"sum_ms,notnull,default:0"`
}

type immersiveLatencyTotalRecord struct {
	bun.BaseModel `bun:"table:metrics_immersive_latency_total,alias:metrics_immersive_latency_total"`

	Metric     string `bun:"metric,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
	SumMS      int64  `bun:"sum_ms,notnull,default:0"`
}

func (c *Collector) RecordImmersive(record ImmersiveRecord) error {
	if c == nil {
		return nil
	}

	counters := make(map[string]map[string]int64)
	add := func(scope, value string) {
		scope = strings.TrimSpace(scope)
		value = strings.TrimSpace(value)
		if scope == "" || value == "" {
			return
		}
		if counters[scope] == nil {
			counters[scope] = make(map[string]int64)
		}
		counters[scope][value]++
	}

	add(immersiveScopeAction, record.Action)
	add(immersiveScopeReason, record.ReasonCode)
	add(immersiveScopeSignalBand, record.SignalBand)
	add(immersiveScopeFastRecovery, record.FastRecoveryReason)
	if kind := strings.TrimSpace(record.ProactiveKind); kind != "" {
		status := strings.TrimSpace(record.ProactiveStatus)
		if status == "" {
			status = "unknown"
		}
		add(immersiveScopeProactive, kind+":"+status)
	}

	hasLatency := record.StrongCallLatencyMS >= 0
	if len(counters) == 0 && !hasLatency {
		return nil
	}

	timestamp := c.normalizeLocal(time.Now())
	dayKey, _ := c.dayHourKey(timestamp)

	return c.db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		if err := upsertImmersiveCounters(ctx, tx, dayKey, counters); err != nil {
			return err
		}
		if hasLatency {
			if err := upsertImmersiveLatency(ctx, tx, dayKey, immersiveLatencyStrongCallReply, record.StrongCallLatencyMS); err != nil {
				return err
			}
		}
		return nil
	})
}

func upsertImmersiveCounters(ctx context.Context, tx bun.Tx, dayKey string, counters map[string]map[string]int64) error {
	if len(counters) == 0 {
		return nil
	}
	scopes := make([]string, 0, len(counters))
	for scope := range counters {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	for _, scope := range scopes {
		values := counters[scope]
		keys := make([]string, 0, len(values))
		for value := range values {
			keys = append(keys, value)
		}
		sort.Strings(keys)

		for _, value := range keys {
			count := values[value]
			if count <= 0 {
				continue
			}
			daily := &immersiveCounterDailyRecord{
				DayKey:     dayKey,
				Scope:      scope,
				Value:      value,
				CountTotal: count,
			}
			if _, err := tx.NewInsert().
				Model(daily).
				On("CONFLICT (day_key, scope, value) DO UPDATE").
				Set("count = metrics_immersive_daily.count + EXCLUDED.count").
				Exec(ctx); err != nil {
				return fmt.Errorf("upsert immersive daily counter failed: %w", err)
			}

			total := &immersiveCounterTotalRecord{
				Scope:      scope,
				Value:      value,
				CountTotal: count,
			}
			if _, err := tx.NewInsert().
				Model(total).
				On("CONFLICT (scope, value) DO UPDATE").
				Set("count = metrics_immersive_total.count + EXCLUDED.count").
				Exec(ctx); err != nil {
				return fmt.Errorf("upsert immersive total counter failed: %w", err)
			}
		}
	}
	return nil
}

func upsertImmersiveLatency(ctx context.Context, tx bun.Tx, dayKey, metric string, latencyMS int64) error {
	if strings.TrimSpace(metric) == "" || latencyMS < 0 {
		return nil
	}

	daily := &immersiveLatencyDailyRecord{
		DayKey:     dayKey,
		Metric:     metric,
		CountTotal: 1,
		SumMS:      latencyMS,
	}
	if _, err := tx.NewInsert().
		Model(daily).
		On("CONFLICT (day_key, metric) DO UPDATE").
		Set("count = metrics_immersive_latency_daily.count + EXCLUDED.count").
		Set("sum_ms = metrics_immersive_latency_daily.sum_ms + EXCLUDED.sum_ms").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert immersive daily latency failed: %w", err)
	}

	total := &immersiveLatencyTotalRecord{
		Metric:     metric,
		CountTotal: 1,
		SumMS:      latencyMS,
	}
	if _, err := tx.NewInsert().
		Model(total).
		On("CONFLICT (metric) DO UPDATE").
		Set("count = metrics_immersive_latency_total.count + EXCLUDED.count").
		Set("sum_ms = metrics_immersive_latency_total.sum_ms + EXCLUDED.sum_ms").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert immersive total latency failed: %w", err)
	}
	return nil
}
