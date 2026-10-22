package handler

import (
	"context"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
	"voex-server/internal/config"
	"voex-server/internal/database"
)

func (h *Handler) RegisterSystemRoutes(public, protected *gin.RouterGroup) {
	public.GET("/health/live", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	public.GET("/health/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()
		if err := h.Service.Readiness(ctx); err != nil {
			c.JSON(503, gin.H{"error": "服务尚未就绪"})
			return
		}
		c.JSON(200, gin.H{"status": "ok", "version": config.Env("VOEX_VERSION", "development"), "schemaVersion": database.LatestVersion})
	})
	protected.GET("/system/info", func(c *gin.Context) {
		enabled := config.Env("VOEX_UPDATER_URL", "") != "" && config.Env("VOEX_UPDATER_TOKEN", "") != ""
		c.JSON(200, gin.H{"version": config.Env("VOEX_VERSION", "development"), "schemaVersion": database.LatestVersion, "upgradeEnabled": enabled, "canUpgrade": enabled})
	})
	protected.GET("/system/releases", h.updaterProxy)
	protected.GET("/system/upgrades/current", h.updaterProxy)
	protected.POST("/system/upgrades", h.updaterProxy)
	protected.POST("/system/packages", h.updaterProxy)
}

// All signed-in users may upgrade in this deployment, per its explicit access policy.
func (h *Handler) updaterProxy(c *gin.Context) {
	target, err := url.Parse(config.Env("VOEX_UPDATER_URL", ""))
	token := config.Env("VOEX_UPDATER_TOKEN", "")
	if err != nil || target.Host == "" || target.Scheme != "http" || token == "" {
		c.JSON(503, gin.H{"error": "升级服务尚未配置"})
		return
	}
	// The upstream and allowed routes come solely from server configuration.
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		r.URL.Path = strings.TrimPrefix(c.Request.URL.Path, "/api/system")
		r.URL.RawPath = ""
		r.URL.RawQuery = ""
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Del("Cookie")
		r.Header.Del("X-Upgrade-Watch-Token")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, _ error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = w.Write([]byte(`{"error":"升级服务暂时不可用，请稍后重试"}`))
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024*1024+1024*1024)
	proxy.ServeHTTP(c.Writer, c.Request)
}
