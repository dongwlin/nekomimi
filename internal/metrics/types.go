package metrics

type LLMStatus struct {
	Enabled bool
	Model   string
}

type Overview struct {
	GeneratedAt   string             `json:"generated_at"`
	Timezone      string             `json:"timezone"`
	Runtime       OverviewRuntime    `json:"runtime"`
	KPI           OverviewKPI        `json:"kpi"`
	TodayInbound  []OverviewTypeItem `json:"today_inbound_types"`
	TodayOutbound []OverviewTypeItem `json:"today_outbound_types"`
	TodayFailed   []OverviewTypeItem `json:"today_failed_types"`
	HourlyTrend   []OverviewHourly   `json:"hourly_trend"`
}

type OverviewRuntime struct {
	ProcessStartedAt string  `json:"process_started_at"`
	BotStartedAt     string  `json:"bot_started_at"`
	BotConnectedAt   *string `json:"bot_connected_at"`
	UptimeSeconds    int64   `json:"uptime_seconds"`
	BotUptimeSeconds int64   `json:"bot_uptime_seconds"`
}

type OverviewKPI struct {
	TodayReceivedTotal int64   `json:"today_received_total"`
	TodaySentTotal     int64   `json:"today_sent_total"`
	TodayFailedTotal   int64   `json:"today_failed_total"`
	TotalReceivedTotal int64   `json:"total_received_total"`
	TotalSentTotal     int64   `json:"total_sent_total"`
	TotalFailedTotal   int64   `json:"total_failed_total"`
	TodayActiveSession int64   `json:"today_active_sessions"`
	LastReceivedAt     *string `json:"last_received_at"`
	LastSentAt         *string `json:"last_sent_at"`
	LastFailedAt       *string `json:"last_failed_at"`
	LLMEnabled         bool    `json:"llm_enabled"`
	LLMModel           string  `json:"llm_model"`
}

type OverviewTypeItem struct {
	Type  string  `json:"type"`
	Count int64   `json:"count"`
	Ratio float64 `json:"ratio"`
}

type OverviewHourly struct {
	Hour     string `json:"hour"`
	Received int64  `json:"received"`
	Sent     int64  `json:"sent"`
	Failed   int64  `json:"failed"`
}
