package security

import (
	"net/http"
	"time"

	"cyberstrike-ai/internal/config"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// NewHTTPRouter creates a router with explicit proxy trust and secret-free access
// and panic logging. Invalid HTTP settings return an error; a nil logger discards
// logs. Request headers, query strings, bodies and panic values are never logged.
func NewHTTPRouter(cfg config.ServerConfig, logger *zap.Logger) (*gin.Engine, error) {
	if err := cfg.ValidateHTTPSecurity(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	router := gin.New()
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}
	router.Use(httpAccessLog(logger), httpRecovery(logger))
	return router, nil
}

func httpAccessLog(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "<unmatched>"
		}
		logger.Info("HTTP request", zap.String("method", c.Request.Method),
			zap.String("route", route), zap.Int("status", c.Writer.Status()),
			zap.String("client_ip", c.ClientIP()), zap.Duration("duration", time.Since(started)))
	}
}

// httpRecovery logs only a stack, never the panic value or a request dump. Already
// written or hijacked streaming responses cannot be replaced with a JSON error.
func httpRecovery(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				logger.Error("HTTP handler panic", zap.Stack("stack"))
				if !c.Writer.Written() {
					c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}

// NewHTTPServer applies bounded request reads and idle waits to either HTTP
// listener. Invalid settings return an error. Writes have no global deadline so
// SSE, WebSocket and MCP streams retain their existing lifetimes.
func NewHTTPServer(address string, handler http.Handler, cfg config.ServerConfig) (*http.Server, error) {
	if err := cfg.ValidateHTTPSecurity(); err != nil {
		return nil, err
	}
	headerTimeout, readTimeout, idleTimeout := cfg.HTTPReadLimits()
	return &http.Server{
		Addr: address, Handler: handler, ReadHeaderTimeout: headerTimeout,
		ReadTimeout: readTimeout, IdleTimeout: idleTimeout,
	}, nil
}
