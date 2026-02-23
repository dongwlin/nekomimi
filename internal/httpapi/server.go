package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/dongwlin/nekomimi/internal/config"
	"github.com/dongwlin/nekomimi/internal/metrics"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

const AuthSQLitePath = "data/auth.db"

type LLMStatusProvider func() metrics.LLMStatus

type RunOptions struct {
	Metrics           *metrics.Collector
	LLMStatusProvider LLMStatusProvider
}

func Run(cfg config.APIConfig, opts RunOptions) error {
	stateStore, initialPassphrase, err := newSQLiteAuthStateStore(AuthSQLitePath)
	if err != nil {
		return fmt.Errorf("initialize auth state store failed: %w", err)
	}
	defer func() {
		if err := stateStore.Close(); err != nil {
			log.Error().Err(err).Msg("close auth state store failed")
		}
	}()

	if initialPassphrase != "" {
		log.Warn().
			Str("passphrase", initialPassphrase).
			Msg("initial api passphrase generated; rotate it immediately")
	}

	authService, err := NewAuthService(AuthServiceConfig{
		KeyHex:      cfg.Auth.PasetoKeyHex,
		AccessTTL:   time.Duration(cfg.Auth.AccessTTLMS) * time.Millisecond,
		RefreshTTL:  time.Duration(cfg.Auth.RefreshTTLMS) * time.Millisecond,
		Passphrases: stateStore,
	})
	if err != nil {
		return fmt.Errorf("initialize auth service failed: %w", err)
	}

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(corsMiddleware(cfg.CORS.AllowOrigins))

	handler := newAuthHandler(authService)
	dashboard := newDashboardHandler(opts.Metrics, opts.LLMStatusProvider)

	v1 := engine.Group("/api/v1")
	{
		auth := v1.Group("/auth")
		{
			auth.POST("/login", handler.login)
			auth.POST("/refresh", handler.refresh)
			auth.GET(
				"/verify",
				accessTokenMiddleware(authService, func(c *gin.Context) {
					c.AbortWithStatusJSON(http.StatusUnauthorized, verifyResponse{
						OK:    false,
						Error: "unauthorized",
					})
				}),
				handler.verify,
			)
			auth.POST("/passphrase/rotate", accessTokenMiddleware(authService, nil), handler.rotatePassphrase)
		}

		v1.GET("/dashboard/overview", accessTokenMiddleware(authService, nil), dashboard.overview)
	}
	mountWebUI(engine)

	server := &http.Server{
		Addr:    cfg.Listen,
		Handler: engine,
	}
	return server.ListenAndServe()
}

func corsMiddleware(allowOrigins []string) gin.HandlerFunc {
	originSet := make(map[string]struct{}, len(allowOrigins))
	for _, origin := range allowOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		originSet[trimmed] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			if _, ok := originSet[origin]; ok {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
				c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type")
				c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			}
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
