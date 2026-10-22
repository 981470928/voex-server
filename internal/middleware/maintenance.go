package middleware

import (
	"github.com/gin-gonic/gin"
	"os"
	"strings"
	"voex-server/internal/config"
)

func Maintenance() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := config.Env("VOEX_MAINTENANCE_FILE", "")
		if path != "" && strings.HasPrefix(c.Request.URL.Path, "/api/") && !strings.HasPrefix(c.Request.URL.Path, "/api/health/") {
			if _, err := os.Stat(path); err == nil || !os.IsNotExist(err) {
				c.Header("Retry-After", "10")
				c.AbortWithStatusJSON(503, gin.H{"error": "系统正在维护，请保留未保存内容，稍后重试"})
				return
			}
		}
		c.Next()
	}
}
