package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMountWebUI_ServesIndexForRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	mountWebUI(engine)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body failed: %v", err)
	}
	if !strings.Contains(string(body), `<div id="root"></div>`) {
		t.Fatalf("expected embedded index html body")
	}
}

func TestMountWebUI_ServesIndexForSPAFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	mountWebUI(engine)

	req := httptest.NewRequest(http.MethodGet, "/settings/security", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
}

func TestMountWebUI_DoesNotHandleAPIPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	mountWebUI(engine)

	req := httptest.NewRequest(http.MethodGet, "/api/unknown", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}

func TestMountWebUI_RejectsNonGETAndHEAD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	mountWebUI(engine)

	req := httptest.NewRequest(http.MethodPost, "/settings/security", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
}
