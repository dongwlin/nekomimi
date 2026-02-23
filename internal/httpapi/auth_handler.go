package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type authHandler struct {
	auth *AuthService
}

func newAuthHandler(auth *AuthService) *authHandler {
	return &authHandler{auth: auth}
}

type loginRequest struct {
	Passphrase string `json:"passphrase" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type rotatePassphraseRequest struct {
	CurrentPassphrase string `json:"current_passphrase" binding:"required"`
	NewPassphrase     string `json:"new_passphrase" binding:"required"`
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type verifyResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

// login godoc
//
//	@Summary		Exchange passphrase for access/refresh token pair
//	@Description	Validate the single passphrase and issue PASETO tokens.
//	@Tags			auth
//	@ID				authLogin
//	@Accept			json
//	@Produce		json
//	@Param			request	body		loginRequest	true	"Passphrase"
//	@Success		200		{object}	tokenResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		500		{object}	errorResponse
//	@Router			/auth/login [post]
func (h *authHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Passphrase) == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid_request"})
		return
	}

	pair, err := h.auth.Login(strings.TrimSpace(req.Passphrase))
	if err != nil {
		if errors.Is(err, ErrInvalidPassphrase) {
			c.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid_passphrase"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		TokenType:        pair.TokenType,
		ExpiresIn:        pair.ExpiresIn,
		RefreshExpiresIn: pair.RefreshExpiresIn,
	})
}

// refresh godoc
//
//	@Summary		Refresh access token with refresh token
//	@Description	Rotate refresh token and issue a new token pair.
//	@Tags			auth
//	@ID				authRefresh
//	@Accept			json
//	@Produce		json
//	@Param			request	body		refreshRequest	true	"Refresh token"
//	@Success		200		{object}	tokenResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		500		{object}	errorResponse
//	@Router			/auth/refresh [post]
func (h *authHandler) refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.RefreshToken) == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid_request"})
		return
	}

	pair, err := h.auth.Refresh(strings.TrimSpace(req.RefreshToken))
	if err != nil {
		if errors.Is(err, ErrInvalidRefreshToken) {
			c.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid_refresh_token"})
			return
		}
		c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		return
	}

	c.JSON(http.StatusOK, tokenResponse{
		AccessToken:      pair.AccessToken,
		RefreshToken:     pair.RefreshToken,
		TokenType:        pair.TokenType,
		ExpiresIn:        pair.ExpiresIn,
		RefreshExpiresIn: pair.RefreshExpiresIn,
	})
}

// verify godoc
//
//	@Summary		Verify access token
//	@Description	Check whether the supplied Bearer access token is valid.
//	@Tags			auth
//	@ID				authVerify
//	@Produce		json
//	@Security		BearerAuth
//	@Success		200	{object}	verifyResponse
//	@Failure		401	{object}	verifyResponse
//	@Router			/auth/verify [get]
func (h *authHandler) verify(c *gin.Context) {
	c.JSON(http.StatusOK, verifyResponse{
		OK: true,
	})
}

// rotate passphrase godoc
//
//	@Summary		Rotate system passphrase
//	@Description	Rotate passphrase with current passphrase and revoke existing sessions immediately.
//	@Tags			auth
//	@ID				authPassphraseRotate
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			request	body		rotatePassphraseRequest	true	"Current and new passphrase"
//	@Success		200		{object}	okResponse
//	@Failure		400		{object}	errorResponse
//	@Failure		401		{object}	errorResponse
//	@Failure		500		{object}	errorResponse
//	@Router			/auth/passphrase/rotate [post]
func (h *authHandler) rotatePassphrase(c *gin.Context) {
	var req rotatePassphraseRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid_request"})
		return
	}

	currentPassphrase := strings.TrimSpace(req.CurrentPassphrase)
	newPassphrase := strings.TrimSpace(req.NewPassphrase)
	if currentPassphrase == "" || newPassphrase == "" {
		c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid_request"})
		return
	}

	err := h.auth.RotatePassphrase(currentPassphrase, newPassphrase)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCurrentPassphrase):
			c.JSON(http.StatusUnauthorized, errorResponse{Error: "invalid_current_passphrase"})
		case errors.Is(err, ErrWeakPassphrase):
			c.JSON(http.StatusBadRequest, errorResponse{Error: "weak_passphrase"})
		default:
			c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal_error"})
		}
		return
	}

	c.JSON(http.StatusOK, okResponse{OK: true})
}
