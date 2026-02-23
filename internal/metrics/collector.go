package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	"github.com/uptrace/bun/driver/sqliteshim"
	zero "github.com/wdvxdr1123/ZeroBot"
)

const metricsStateRowID = 1

type Collector struct {
	mu sync.RWMutex

	db    *bun.DB
	sqlDB *sql.DB

	location *time.Location

	processStartedAt time.Time
	botStartedAt     time.Time
}

type dailyRecord struct {
	bun.BaseModel `bun:"table:metrics_daily,alias:metrics_daily"`

	DayKey        string `bun:"day_key,pk,type:text"`
	ReceivedTotal int64  `bun:"received_total,notnull,default:0"`
	SentTotal     int64  `bun:"sent_total,notnull,default:0"`
	FailedTotal   int64  `bun:"failed_total,notnull,default:0"`
}

type hourlyRecord struct {
	bun.BaseModel `bun:"table:metrics_hourly,alias:metrics_hourly"`

	DayKey        string `bun:"day_key,pk,type:text"`
	HourKey       string `bun:"hour_key,pk,type:text"`
	ReceivedTotal int64  `bun:"received_total,notnull,default:0"`
	SentTotal     int64  `bun:"sent_total,notnull,default:0"`
	FailedTotal   int64  `bun:"failed_total,notnull,default:0"`
}

type typeDailyRecord struct {
	bun.BaseModel `bun:"table:metrics_type_daily,alias:metrics_type_daily"`

	DayKey     string `bun:"day_key,pk,type:text"`
	Direction  string `bun:"direction,pk,type:text"`
	Success    int8   `bun:"success,pk,type:integer"`
	TypeKey    string `bun:"type_key,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
}

type typeTotalRecord struct {
	bun.BaseModel `bun:"table:metrics_type_total,alias:metrics_type_total"`

	Direction  string `bun:"direction,pk,type:text"`
	Success    int8   `bun:"success,pk,type:integer"`
	TypeKey    string `bun:"type_key,pk,type:text"`
	CountTotal int64  `bun:"count,notnull,default:0"`
}

type dailySessionRecord struct {
	bun.BaseModel `bun:"table:metrics_daily_sessions,alias:metrics_daily_sessions"`

	DayKey     string `bun:"day_key,pk,type:text"`
	SessionKey string `bun:"session_key,pk,type:text"`
}

type stateRecord struct {
	bun.BaseModel `bun:"table:metrics_state,alias:metrics_state"`

	ID           int64      `bun:"id,pk"`
	LastReceived *time.Time `bun:"last_received_at,type:datetime,nullzero"`
	LastSent     *time.Time `bun:"last_sent_at,type:datetime,nullzero"`
	LastFailed   *time.Time `bun:"last_failed_at,type:datetime,nullzero"`
	BotConnected *time.Time `bun:"bot_connected_at,type:datetime,nullzero"`
}

type totalsRow struct {
	ReceivedTotal int64 `bun:"received_total"`
	SentTotal     int64 `bun:"sent_total"`
	FailedTotal   int64 `bun:"failed_total"`
}

func NewCollector(dbPath string) (*Collector, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("sqlite path is empty")
	}

	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create metrics sqlite directory failed: %w", err)
		}
	}

	sqlDB, err := sql.Open(sqliteshim.ShimName, sqliteDSN(dbPath))
	if err != nil {
		return nil, fmt.Errorf("open metrics sqlite failed: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	db := bun.NewDB(sqlDB, sqlitedialect.New())

	collector := &Collector{
		db:       db,
		sqlDB:    sqlDB,
		location: time.Now().Location(),
	}
	if err := collector.initialize(context.Background()); err != nil {
		_ = collector.Close()
		return nil, err
	}

	return collector, nil
}

func (c *Collector) Close() error {
	if c == nil || c.sqlDB == nil {
		return nil
	}
	return c.sqlDB.Close()
}

func (c *Collector) initialize(ctx context.Context) error {
	models := []interface{}{
		(*dailyRecord)(nil),
		(*hourlyRecord)(nil),
		(*typeDailyRecord)(nil),
		(*typeTotalRecord)(nil),
		(*dailySessionRecord)(nil),
		(*stateRecord)(nil),
	}
	for _, model := range models {
		if _, err := c.db.NewCreateTable().Model(model).IfNotExists().Exec(ctx); err != nil {
			return fmt.Errorf("create metrics table failed: %w", err)
		}
	}

	if _, err := c.db.NewInsert().Model(&stateRecord{ID: metricsStateRowID}).On("CONFLICT (id) DO NOTHING").Exec(ctx); err != nil {
		return fmt.Errorf("initialize metrics state failed: %w", err)
	}
	return nil
}

func (c *Collector) SetProcessStartedAt(ts time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.processStartedAt = c.normalizeLocal(ts)
	c.mu.Unlock()
}

func (c *Collector) SetBotStartedAt(ts time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.botStartedAt = c.normalizeLocal(ts)
	c.mu.Unlock()
}

func (c *Collector) MarkBotConnected(ts time.Time) error {
	if c == nil {
		return nil
	}
	connectedAt := c.normalizeLocal(ts)
	_, err := c.db.NewUpdate().
		Model((*stateRecord)(nil)).
		Set("bot_connected_at = COALESCE(bot_connected_at, ?)", connectedAt).
		Where("id = ?", metricsStateRowID).
		Exec(context.Background())
	if err != nil {
		return fmt.Errorf("mark bot connected failed: %w", err)
	}
	return nil
}

func (c *Collector) RecordInbound(event *zero.Event, sessionKey string) error {
	if c == nil || event == nil {
		return nil
	}

	typeKeys := InboundTypeKeys(event)
	if len(typeKeys) == 0 {
		return nil
	}
	typeCounts := summarizeTypeCounts(typeKeys)
	increment := sumTypeCounts(typeCounts)
	timestamp := c.eventTimestamp(event)
	dayKey, hourKey := c.dayHourKey(timestamp)
	session := strings.TrimSpace(sessionKey)

	return c.db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		if err := upsertDaily(ctx, tx, dayKey, increment, 0, 0); err != nil {
			return err
		}
		if err := upsertHourly(ctx, tx, dayKey, hourKey, increment, 0, 0); err != nil {
			return err
		}
		if err := upsertTypeCounters(ctx, tx, dayKey, directionInbound, 1, typeCounts); err != nil {
			return err
		}
		if session != "" {
			if err := upsertDailySession(ctx, tx, dayKey, session); err != nil {
				return err
			}
		}
		if _, err := tx.NewUpdate().
			Model((*stateRecord)(nil)).
			Set("last_received_at = ?", timestamp).
			Set("bot_connected_at = COALESCE(bot_connected_at, ?)", timestamp).
			Where("id = ?", metricsStateRowID).
			Exec(ctx); err != nil {
			return fmt.Errorf("update inbound state failed: %w", err)
		}
		return nil
	})
}

func (c *Collector) RecordOutbound(typeKeys []string, success bool, sessionKey string) error {
	if c == nil {
		return nil
	}

	if len(typeKeys) == 0 {
		typeKeys = []string{typeOutboundPref + "other"}
	}
	typeCounts := summarizeTypeCounts(typeKeys)
	increment := sumTypeCounts(typeCounts)
	timestamp := c.normalizeLocal(time.Now())
	dayKey, hourKey := c.dayHourKey(timestamp)
	session := strings.TrimSpace(sessionKey)
	successValue := int8(0)
	sentInc := int64(0)
	failedInc := int64(0)
	if success {
		successValue = 1
		sentInc = increment
	} else {
		failedInc = increment
	}

	return c.db.RunInTx(context.Background(), nil, func(ctx context.Context, tx bun.Tx) error {
		if err := upsertDaily(ctx, tx, dayKey, 0, sentInc, failedInc); err != nil {
			return err
		}
		if err := upsertHourly(ctx, tx, dayKey, hourKey, 0, sentInc, failedInc); err != nil {
			return err
		}
		if err := upsertTypeCounters(ctx, tx, dayKey, directionOutbound, successValue, typeCounts); err != nil {
			return err
		}
		if session != "" {
			if err := upsertDailySession(ctx, tx, dayKey, session); err != nil {
				return err
			}
		}

		update := tx.NewUpdate().Model((*stateRecord)(nil)).Where("id = ?", metricsStateRowID)
		if success {
			update.Set("last_sent_at = ?", timestamp)
		} else {
			update.Set("last_failed_at = ?", timestamp)
		}
		if _, err := update.Exec(ctx); err != nil {
			return fmt.Errorf("update outbound state failed: %w", err)
		}
		return nil
	})
}

func (c *Collector) BuildOverview(now time.Time, llmStatus LLMStatus) (Overview, error) {
	if c == nil {
		return Overview{}, fmt.Errorf("collector is nil")
	}

	generatedAt := c.normalizeLocal(now)
	dayKey, _ := c.dayHourKey(generatedAt)

	todayRow := dailyRecord{}
	err := c.db.NewSelect().
		Model(&todayRow).
		Where("day_key = ?", dayKey).
		Limit(1).
		Scan(context.Background())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Overview{}, fmt.Errorf("load today metrics failed: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		todayRow = dailyRecord{}
	}

	totals := totalsRow{}
	if err := c.db.NewSelect().
		TableExpr("metrics_daily").
		ColumnExpr("COALESCE(SUM(received_total), 0) AS received_total").
		ColumnExpr("COALESCE(SUM(sent_total), 0) AS sent_total").
		ColumnExpr("COALESCE(SUM(failed_total), 0) AS failed_total").
		Scan(context.Background(), &totals); err != nil {
		return Overview{}, fmt.Errorf("load metrics totals failed: %w", err)
	}

	activeSessions, err := c.db.NewSelect().
		Model((*dailySessionRecord)(nil)).
		Where("day_key = ?", dayKey).
		Count(context.Background())
	if err != nil {
		return Overview{}, fmt.Errorf("load active sessions failed: %w", err)
	}

	state := stateRecord{}
	err = c.db.NewSelect().
		Model(&state).
		Where("id = ?", metricsStateRowID).
		Limit(1).
		Scan(context.Background())
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Overview{}, fmt.Errorf("load metrics state failed: %w", err)
	}
	if errors.Is(err, sql.ErrNoRows) {
		state = stateRecord{}
	}

	todayInbound, err := c.loadTypeRows(dayKey, directionInbound, 1)
	if err != nil {
		return Overview{}, err
	}
	todayOutbound, err := c.loadTypeRows(dayKey, directionOutbound, 1)
	if err != nil {
		return Overview{}, err
	}
	todayFailed, err := c.loadTypeRows(dayKey, directionOutbound, 0)
	if err != nil {
		return Overview{}, err
	}

	hourlyTrend, err := c.loadHourlyTrend(dayKey)
	if err != nil {
		return Overview{}, err
	}

	processStartedAt, botStartedAt := c.runtimeStarts()

	return Overview{
		GeneratedAt: generatedAt.Format(time.RFC3339),
		Timezone:    c.location.String(),
		Runtime: OverviewRuntime{
			ProcessStartedAt: formatTime(processStartedAt),
			BotStartedAt:     formatTime(botStartedAt),
			BotConnectedAt:   formatTimePtr(state.BotConnected),
			UptimeSeconds:    secondsSince(processStartedAt, generatedAt),
			BotUptimeSeconds: secondsSince(botStartedAt, generatedAt),
		},
		KPI: OverviewKPI{
			TodayReceivedTotal: todayRow.ReceivedTotal,
			TodaySentTotal:     todayRow.SentTotal,
			TodayFailedTotal:   todayRow.FailedTotal,
			TotalReceivedTotal: totals.ReceivedTotal,
			TotalSentTotal:     totals.SentTotal,
			TotalFailedTotal:   totals.FailedTotal,
			TodayActiveSession: int64(activeSessions),
			LastReceivedAt:     formatTimePtr(state.LastReceived),
			LastSentAt:         formatTimePtr(state.LastSent),
			LastFailedAt:       formatTimePtr(state.LastFailed),
			LLMEnabled:         llmStatus.Enabled,
			LLMProvider:        llmStatus.Provider,
			LLMModel:           llmStatus.Model,
		},
		TodayInbound:  todayInbound,
		TodayOutbound: todayOutbound,
		TodayFailed:   todayFailed,
		HourlyTrend:   hourlyTrend,
	}, nil
}

func (c *Collector) loadTypeRows(dayKey, direction string, success int8) ([]OverviewTypeItem, error) {
	rows := make([]typeDailyRecord, 0)
	if err := c.db.NewSelect().
		Model(&rows).
		Where("day_key = ? AND direction = ? AND success = ?", dayKey, direction, success).
		OrderExpr("count DESC").
		OrderExpr("type_key ASC").
		Scan(context.Background()); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load type rows failed: %w", err)
	}
	if len(rows) == 0 {
		return []OverviewTypeItem{}, nil
	}

	total := int64(0)
	for _, row := range rows {
		total += row.CountTotal
	}

	items := make([]OverviewTypeItem, 0, len(rows))
	for _, row := range rows {
		ratio := 0.0
		if total > 0 {
			ratio = float64(row.CountTotal) / float64(total)
		}
		items = append(items, OverviewTypeItem{
			Type:  row.TypeKey,
			Count: row.CountTotal,
			Ratio: ratio,
		})
	}
	return items, nil
}

func (c *Collector) loadHourlyTrend(dayKey string) ([]OverviewHourly, error) {
	rows := make([]hourlyRecord, 0)
	if err := c.db.NewSelect().
		Model(&rows).
		Where("day_key = ?", dayKey).
		Scan(context.Background()); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("load hourly metrics failed: %w", err)
	}

	byHour := make(map[string]hourlyRecord, len(rows))
	for _, row := range rows {
		byHour[row.HourKey] = row
	}

	trend := make([]OverviewHourly, 0, 24)
	for i := range 24 {
		hourKey := fmt.Sprintf("%02d", i)
		row := byHour[hourKey]
		trend = append(trend, OverviewHourly{
			Hour:     hourKey + ":00",
			Received: row.ReceivedTotal,
			Sent:     row.SentTotal,
			Failed:   row.FailedTotal,
		})
	}
	return trend, nil
}

func upsertDaily(ctx context.Context, tx bun.Tx, dayKey string, received, sent, failed int64) error {
	record := &dailyRecord{
		DayKey:        dayKey,
		ReceivedTotal: received,
		SentTotal:     sent,
		FailedTotal:   failed,
	}
	if _, err := tx.NewInsert().
		Model(record).
		On("CONFLICT (day_key) DO UPDATE").
		Set("received_total = metrics_daily.received_total + EXCLUDED.received_total").
		Set("sent_total = metrics_daily.sent_total + EXCLUDED.sent_total").
		Set("failed_total = metrics_daily.failed_total + EXCLUDED.failed_total").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert daily metrics failed: %w", err)
	}
	return nil
}

func upsertHourly(ctx context.Context, tx bun.Tx, dayKey, hourKey string, received, sent, failed int64) error {
	record := &hourlyRecord{
		DayKey:        dayKey,
		HourKey:       hourKey,
		ReceivedTotal: received,
		SentTotal:     sent,
		FailedTotal:   failed,
	}
	if _, err := tx.NewInsert().
		Model(record).
		On("CONFLICT (day_key, hour_key) DO UPDATE").
		Set("received_total = metrics_hourly.received_total + EXCLUDED.received_total").
		Set("sent_total = metrics_hourly.sent_total + EXCLUDED.sent_total").
		Set("failed_total = metrics_hourly.failed_total + EXCLUDED.failed_total").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert hourly metrics failed: %w", err)
	}
	return nil
}

func upsertTypeCounters(ctx context.Context, tx bun.Tx, dayKey, direction string, success int8, counts map[string]int64) error {
	if len(counts) == 0 {
		return nil
	}

	typeKeys := make([]string, 0, len(counts))
	for typeKey := range counts {
		typeKeys = append(typeKeys, typeKey)
	}
	sort.Strings(typeKeys)

	for _, typeKey := range typeKeys {
		count := counts[typeKey]
		if count <= 0 {
			continue
		}
		daily := &typeDailyRecord{
			DayKey:     dayKey,
			Direction:  direction,
			Success:    success,
			TypeKey:    typeKey,
			CountTotal: count,
		}
		if _, err := tx.NewInsert().
			Model(daily).
			On("CONFLICT (day_key, direction, success, type_key) DO UPDATE").
			Set("count = metrics_type_daily.count + EXCLUDED.count").
			Exec(ctx); err != nil {
			return fmt.Errorf("upsert daily type metrics failed: %w", err)
		}

		total := &typeTotalRecord{
			Direction:  direction,
			Success:    success,
			TypeKey:    typeKey,
			CountTotal: count,
		}
		if _, err := tx.NewInsert().
			Model(total).
			On("CONFLICT (direction, success, type_key) DO UPDATE").
			Set("count = metrics_type_total.count + EXCLUDED.count").
			Exec(ctx); err != nil {
			return fmt.Errorf("upsert total type metrics failed: %w", err)
		}
	}
	return nil
}

func upsertDailySession(ctx context.Context, tx bun.Tx, dayKey, sessionKey string) error {
	record := &dailySessionRecord{
		DayKey:     dayKey,
		SessionKey: sessionKey,
	}
	if _, err := tx.NewInsert().
		Model(record).
		On("CONFLICT (day_key, session_key) DO NOTHING").
		Exec(ctx); err != nil {
		return fmt.Errorf("upsert daily session failed: %w", err)
	}
	return nil
}

func summarizeTypeCounts(typeKeys []string) map[string]int64 {
	counts := make(map[string]int64, len(typeKeys))
	for _, key := range typeKeys {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		counts[trimmed]++
	}
	return counts
}

func sumTypeCounts(counts map[string]int64) int64 {
	total := int64(0)
	for _, count := range counts {
		total += count
	}
	return total
}

func (c *Collector) dayHourKey(ts time.Time) (string, string) {
	local := c.normalizeLocal(ts)
	return local.Format("2006-01-02"), local.Format("15")
}

func (c *Collector) eventTimestamp(event *zero.Event) time.Time {
	if event == nil || event.Time <= 0 {
		return c.normalizeLocal(time.Now())
	}
	return c.normalizeLocal(time.Unix(event.Time, 0))
}

func (c *Collector) normalizeLocal(ts time.Time) time.Time {
	if ts.IsZero() {
		ts = time.Now()
	}
	return ts.In(c.location)
}

func (c *Collector) runtimeStarts() (time.Time, time.Time) {
	c.mu.RLock()
	processStartedAt := c.processStartedAt
	botStartedAt := c.botStartedAt
	c.mu.RUnlock()
	return processStartedAt, botStartedAt
}

func formatTime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}

func formatTimePtr(ts *time.Time) *string {
	if ts == nil || ts.IsZero() {
		return nil
	}
	value := ts.Format(time.RFC3339)
	return &value
}

func secondsSince(startedAt, now time.Time) int64 {
	if startedAt.IsZero() {
		return 0
	}
	if now.Before(startedAt) {
		return 0
	}
	return int64(now.Sub(startedAt) / time.Second)
}

func sqliteDSN(path string) string {
	normalized := filepath.ToSlash(path)
	return "file:" + normalized + "?_pragma=busy_timeout(5000)"
}
