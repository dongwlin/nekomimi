package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

const testPasetoKeyHex = "9f10ec4ee8ca74d6b6a6460f6609409e63d76ca4bc5f8cc86f3bd9464f694f16"
const uuidV4Pattern = `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`

func buildTestAuthService(t *testing.T, accessTTL time.Duration, refreshTTL time.Duration) (*AuthService, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "auth.db")
	stateStore, initialPassphrase, err := newSQLiteAuthStateStore(dbPath)
	if err != nil {
		t.Fatalf("new auth state store failed: %v", err)
	}
	t.Cleanup(func() {
		_ = stateStore.Close()
	})

	service, err := NewAuthService(AuthServiceConfig{
		KeyHex:      testPasetoKeyHex,
		AccessTTL:   accessTTL,
		RefreshTTL:  refreshTTL,
		Passphrases: stateStore,
	})
	if err != nil {
		t.Fatalf("new auth service failed: %v", err)
	}
	if initialPassphrase == "" {
		t.Fatal("expected initial passphrase to be generated")
	}
	return service, initialPassphrase
}

func TestAuthService_LoginAndVerify(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)

	pair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("expected token pair, got %#v", pair)
	}

	if err := service.VerifyAccessToken(pair.AccessToken); err != nil {
		t.Fatalf("verify access failed: %v", err)
	}
}

func TestAuthService_JTIIsUUIDv4(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)
	pair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	matcher := regexp.MustCompile(uuidV4Pattern)

	accessClaims, err := service.parseAndValidateToken(pair.AccessToken, tokenTypeAccess)
	if err != nil {
		t.Fatalf("parse access token failed: %v", err)
	}
	if !matcher.MatchString(accessClaims.JTI) {
		t.Fatalf("access jti is not uuid v4: %q", accessClaims.JTI)
	}
	if accessClaims.Version != 1 {
		t.Fatalf("unexpected access token version: %d", accessClaims.Version)
	}

	refreshClaims, err := service.parseAndValidateToken(pair.RefreshToken, tokenTypeRefresh)
	if err != nil {
		t.Fatalf("parse refresh token failed: %v", err)
	}
	if !matcher.MatchString(refreshClaims.JTI) {
		t.Fatalf("refresh jti is not uuid v4: %q", refreshClaims.JTI)
	}
	if refreshClaims.Version != 1 {
		t.Fatalf("unexpected refresh token version: %d", refreshClaims.Version)
	}
}

func TestAuthService_LoginRejectsInvalidPassphrase(t *testing.T) {
	service, _ := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)

	_, err := service.Login("invalid")
	if err != ErrInvalidPassphrase {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuthService_AccessTokenExpires(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 20*time.Millisecond, 7*24*time.Hour)

	pair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	time.Sleep(30 * time.Millisecond)

	if err := service.VerifyAccessToken(pair.AccessToken); err != ErrUnauthorized {
		t.Fatalf("expected unauthorized after expiry, got: %v", err)
	}
}

func TestAuthService_RefreshRotatesToken(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)

	firstPair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	secondPair, err := service.Refresh(firstPair.RefreshToken)
	if err != nil {
		t.Fatalf("refresh failed: %v", err)
	}
	if secondPair.AccessToken == firstPair.AccessToken {
		t.Fatalf("expected new access token after refresh")
	}
	if secondPair.RefreshToken == firstPair.RefreshToken {
		t.Fatalf("expected new refresh token after refresh")
	}

	if _, err := service.Refresh(firstPair.RefreshToken); err != ErrInvalidRefreshToken {
		t.Fatalf("expected rotated refresh token to be invalid, got %v", err)
	}
}

func TestAuthService_RotatePassphraseRevokesTokens(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)
	firstPair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	newPassphrase := "new-secret-password"
	if err := service.RotatePassphrase(passphrase, newPassphrase); err != nil {
		t.Fatalf("rotate passphrase failed: %v", err)
	}

	if err := service.VerifyAccessToken(firstPair.AccessToken); err != ErrUnauthorized {
		t.Fatalf("expected old access token to be revoked, got %v", err)
	}
	if _, err := service.Refresh(firstPair.RefreshToken); err != ErrInvalidRefreshToken {
		t.Fatalf("expected old refresh token to be revoked, got %v", err)
	}
	if _, err := service.Login(passphrase); err != ErrInvalidPassphrase {
		t.Fatalf("expected old passphrase to be invalid, got %v", err)
	}

	secondPair, err := service.Login(newPassphrase)
	if err != nil {
		t.Fatalf("login with new passphrase failed: %v", err)
	}
	secondClaims, err := service.parseAndValidateToken(secondPair.AccessToken, tokenTypeAccess)
	if err != nil {
		t.Fatalf("parse new access token failed: %v", err)
	}
	if secondClaims.Version != 2 {
		t.Fatalf("expected token version 2 after rotation, got %d", secondClaims.Version)
	}
}

func TestAuthService_RotatePassphrasePolicy(t *testing.T) {
	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)

	if err := service.RotatePassphrase("wrong-current", "next-password"); err != ErrInvalidCurrentPassphrase {
		t.Fatalf("expected invalid current passphrase error, got %v", err)
	}
	if err := service.RotatePassphrase(passphrase, "short"); err != ErrWeakPassphrase {
		t.Fatalf("expected weak passphrase error, got %v", err)
	}
}

func TestAccessTokenMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	service, passphrase := buildTestAuthService(t, 15*time.Minute, 7*24*time.Hour)
	pair, err := service.Login(passphrase)
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}

	engine := gin.New()
	engine.GET("/verify", accessTokenMiddleware(service, nil), func(c *gin.Context) {
		c.JSON(http.StatusOK, verifyResponse{OK: true})
	})

	t.Run("missing authorization", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
	})

	t.Run("invalid bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		req.Header.Set("Authorization", "Bearer invalid")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
	})

	t.Run("valid bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/verify", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("unexpected status: %d", rec.Code)
		}
	})
}
