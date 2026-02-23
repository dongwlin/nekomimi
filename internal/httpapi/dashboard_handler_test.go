package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/gin-gonic/gin"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func buildDashboardEngine(t *testing.T) (*gin.Engine, *AuthService, string, *metrics.Collector) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)
	collector, err := metrics.NewCollector(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatalf("new metrics collector failed: %v", err)
	}
	t.Cleanup(func() {
		_ = collector.Close()
	})
	collector.SetProcessStartedAt(time.Now().Add(-2 * time.Hour))
	collector.SetBotStartedAt(time.Now().Add(-1 * time.Hour))

	_ = collector.RecordInbound(&zero.Event{
		Time:     time.Now().Unix(),
		PostType: "message",
		Message: message.Message{
			{Type: "text", Data: map[string]string{"text": "ping"}},
		},
	}, "group:1")

	handler := newDashboardHandler(collector, func() metrics.LLMStatus {
		return metrics.LLMStatus{
			Enabled:  true,
			Provider: "responses",
			Model:    "gpt-4.1-mini",
		}
	})

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	v1.GET("/dashboard/overview", accessTokenMiddleware(service, nil), handler.overview)
	return engine, service, passphrase, collector
}

func TestDashboardHandlerOverview(t *testing.T) {
	engine, service, passphrase, _ := buildDashboardEngine(t)

	t.Run("missing token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
	})

	t.Run("valid token", func(t *testing.T) {
		pair, err := service.Login(passphrase)
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/overview", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
		}

		var body metrics.Overview
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body failed: %v", err)
		}
		if body.GeneratedAt == "" || body.Timezone == "" {
			t.Fatalf("missing generated_at/timezone: %+v", body)
		}
		if body.Runtime.ProcessStartedAt == "" || body.Runtime.BotStartedAt == "" {
			t.Fatalf("missing runtime start timestamps: %+v", body.Runtime)
		}
		if len(body.HourlyTrend) != 24 {
			t.Fatalf("unexpected hourly trend length: %d", len(body.HourlyTrend))
		}
		if body.KPI.LLMProvider != "responses" || body.KPI.LLMModel != "gpt-4.1-mini" || !body.KPI.LLMEnabled {
			t.Fatalf("unexpected llm status in response: %+v", body.KPI)
		}
	})
}
