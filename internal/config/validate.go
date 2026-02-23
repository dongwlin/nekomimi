package config

import (
	"fmt"
	"strings"

	paseto "aidanwoods.dev/go-paseto"
)

const (
	defaultAPIListen     = "127.0.0.1:8080"
	defaultAccessTTLMS   = 15 * 60 * 1000
	defaultRefreshTTLMS  = 7 * 24 * 60 * 60 * 1000
	defaultWebOrigin     = "http://localhost:5173"
	minAllowedTokenTTLMS = 1
)

func applyDefaults(cfg *Config) {
	if cfg.API.Listen == "" {
		cfg.API.Listen = defaultAPIListen
	}
	if cfg.API.Auth.AccessTTLMS == 0 {
		cfg.API.Auth.AccessTTLMS = defaultAccessTTLMS
	}
	if cfg.API.Auth.RefreshTTLMS == 0 {
		cfg.API.Auth.RefreshTTLMS = defaultRefreshTTLMS
	}
	if len(cfg.API.CORS.AllowOrigins) == 0 {
		cfg.API.CORS.AllowOrigins = []string{defaultWebOrigin}
	}
}

func validate(cfg *Config) error {
	if !cfg.API.Enabled {
		return nil
	}

	cfg.API.Auth.PasetoKeyHex = strings.TrimSpace(cfg.API.Auth.PasetoKeyHex)
	if cfg.API.Auth.AccessTTLMS < minAllowedTokenTTLMS {
		return fmt.Errorf("api.auth.access_ttl_ms must be greater than 0")
	}
	if cfg.API.Auth.RefreshTTLMS < minAllowedTokenTTLMS {
		return fmt.Errorf("api.auth.refresh_ttl_ms must be greater than 0")
	}
	if cfg.API.Auth.PasetoKeyHex != "" {
		if _, err := paseto.V4SymmetricKeyFromHex(cfg.API.Auth.PasetoKeyHex); err != nil {
			return fmt.Errorf("api.auth.paseto_key_hex is invalid: %w", err)
		}
	}

	return nil
}
