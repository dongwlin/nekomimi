package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type unauthorizedResponseWriter func(c *gin.Context)

func accessTokenMiddleware(auth *AuthService, writeUnauthorized unauthorizedResponseWriter) gin.HandlerFunc {
	if writeUnauthorized == nil {
		writeUnauthorized = func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, errorResponse{Error: "unauthorized"})
		}
	}

	return func(c *gin.Context) {
		token, err := ParseBearerToken(c.GetHeader("Authorization"))
		if err != nil {
			writeUnauthorized(c)
			return
		}
		if err := auth.VerifyAccessToken(token); err != nil {
			writeUnauthorized(c)
			return
		}
		c.Next()
	}
}
