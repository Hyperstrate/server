package httpserver

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hyperstrate/server/docs"
	"hyperstrate/server/internal/config"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	swaggerfiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

// slogRequestLogger is a Gin middleware that logs each HTTP request via slog.
// It replaces gin.Logger() so all request logs share the same structured sink.
//   - 5xx → slog.Error  (server fault — operator needs to investigate)
//   - 4xx → slog.Warn   (client error — useful for debugging abuse / bad clients)
//   - 2xx/3xx → slog.Info (normal traffic; silence /healthz to reduce noise)
func slogRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		// Skip healthcheck to avoid log spam.
		if path == "/healthz" && status < 400 {
			return
		}

		attrs := []any{
			"method", c.Request.Method,
			"path", path,
			"status", status,
			"latency", latency.String(),
			"ip", c.ClientIP(),
		}
		if query != "" {
			attrs = append(attrs, "query", query)
		}
		if errs := c.Errors.String(); errs != "" {
			attrs = append(attrs, "ginErrors", errs)
		}

		switch {
		case status >= 500:
			slog.Error("request", attrs...)
		case status >= 400:
			slog.Warn("request", attrs...)
		default:
			slog.Info("request", attrs...)
		}
	}
}

func NewRouter(cfg config.Config) *gin.Engine {
	router := gin.New()
	// gin.Recovery() is kept for panic safety; gin.Logger() is replaced by slogRequestLogger.
	router.Use(slogRequestLogger(), gin.Recovery())

	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{cfg.FrontendURL},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	configureSwaggerInfo(cfg)

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerfiles.Handler))

	return router
}

func configureSwaggerInfo(cfg config.Config) {
	publicURL := strings.TrimSpace(cfg.APIPublicURL)
	if publicURL == "" {
		port := cfg.Port
		if port == "" {
			port = config.DefaultPort
		}
		publicURL = fmt.Sprintf("http://localhost:%s", port)
	}

	parsed, err := url.Parse(publicURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		docs.SwaggerInfo.Host = fmt.Sprintf("localhost:%s", config.DefaultPort)
		docs.SwaggerInfo.Schemes = []string{"http"}
		docs.SwaggerInfo.BasePath = "/"
		return
	}

	docs.SwaggerInfo.Host = parsed.Host
	docs.SwaggerInfo.Schemes = []string{parsed.Scheme}
	if path := strings.TrimRight(parsed.Path, "/"); path != "" {
		docs.SwaggerInfo.BasePath = path
		return
	}
	docs.SwaggerInfo.BasePath = "/"
}
