package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type httpResponseBody map[string]any

func buildAuthHandlerEngine(t *testing.T) (*gin.Engine, *AuthService, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)
	handler := newAuthHandler(service)

	engine := gin.New()
	v1 := engine.Group("/api/v1")
	auth := v1.Group("/auth")
	auth.POST("/login", handler.login)
	auth.POST("/refresh", handler.refresh)
	auth.GET(
		"/verify",
		accessTokenMiddleware(service, func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, verifyResponse{
				OK:    false,
				Error: "unauthorized",
			})
		}),
		handler.verify,
	)
	auth.POST("/passphrase/rotate", accessTokenMiddleware(service, nil), handler.rotatePassphrase)
	return engine, service, passphrase
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) httpResponseBody {
	t.Helper()
	var body httpResponseBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body failed: %v", err)
	}
	return body
}

func TestAuthHandlerLogin(t *testing.T) {
	engine, _, passphrase := buildAuthHandlerEngine(t)

	t.Run("invalid passphrase", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"passphrase":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["error"] != "invalid_passphrase" {
			t.Fatalf("unexpected error body: %#v", body)
		}
	})

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"passphrase":"`+passphrase+`"}`))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["access_token"] == "" || body["refresh_token"] == "" {
			t.Fatalf("missing tokens in body: %#v", body)
		}
		if body["token_type"] != "Bearer" {
			t.Fatalf("unexpected token_type: %#v", body["token_type"])
		}
	})
}

func TestAuthHandlerRefresh(t *testing.T) {
	engine, service, passphrase := buildAuthHandlerEngine(t)

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBufferString(`{"refresh_token":"bad-token"}`))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["error"] != "invalid_refresh_token" {
			t.Fatalf("unexpected error body: %#v", body)
		}
	})

	t.Run("success", func(t *testing.T) {
		pair, err := service.Login(passphrase)
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		reqBody := map[string]string{
			"refresh_token": pair.RefreshToken,
		}
		data, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["access_token"] == "" || body["refresh_token"] == "" {
			t.Fatalf("missing refreshed tokens in body: %#v", body)
		}
	})
}

func TestAuthHandlerVerify(t *testing.T) {
	engine, service, passphrase := buildAuthHandlerEngine(t)

	t.Run("missing authorization", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["ok"] != false || body["error"] != "unauthorized" {
			t.Fatalf("unexpected body: %#v", body)
		}
	})

	t.Run("valid access token", func(t *testing.T) {
		pair, err := service.Login(passphrase)
		if err != nil {
			t.Fatalf("login failed: %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/verify", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["ok"] != true {
			t.Fatalf("unexpected body: %#v", body)
		}
	})
}

func TestAuthHandlerRotatePassphrase(t *testing.T) {
	engine, service, passphrase := buildAuthHandlerEngine(t)
	pair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	t.Run("missing bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passphrase/rotate", bytes.NewBufferString(`{"current_passphrase":"x","new_passphrase":"abcdefgh"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["error"] != "unauthorized" {
			t.Fatalf("unexpected body: %#v", body)
		}
	})

	t.Run("invalid current passphrase", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passphrase/rotate", bytes.NewBufferString(`{"current_passphrase":"wrong","new_passphrase":"abcdefgh"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["error"] != "invalid_current_passphrase" {
			t.Fatalf("unexpected body: %#v", body)
		}
	})

	t.Run("weak passphrase", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passphrase/rotate", bytes.NewBufferString(`{"current_passphrase":"`+passphrase+`","new_passphrase":"short"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["error"] != "weak_passphrase" {
			t.Fatalf("unexpected body: %#v", body)
		}
	})

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/passphrase/rotate", bytes.NewBufferString(`{"current_passphrase":"`+passphrase+`","new_passphrase":"new-password-123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
		body := decodeBody(t, rec)
		if body["ok"] != true {
			t.Fatalf("unexpected body: %#v", body)
		}
	})
}
