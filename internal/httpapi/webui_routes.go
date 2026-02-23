package httpapi

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/dongwlin/nekomimi/internal/httpapi/webui"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

func mountWebUI(engine *gin.Engine) {
	distFS, err := webui.DistFS()
	if err != nil {
		log.Warn().Err(err).Msg("embedded web ui is unavailable")
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	engine.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		assetPath := strings.TrimPrefix(c.Request.URL.Path, "/")
		if assetPath != "" {
			if _, err := fs.Stat(distFS, assetPath); err == nil {
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		indexReq := c.Request.Clone(c.Request.Context())
		indexReq.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, indexReq)
	})
}
