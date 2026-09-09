package provider

import (
	"github.com/alecthw/sub-server/internal/filestore"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// Handler handles GET /provider/:provider.
func Handler(providerDir string, client *resty.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		handle(c, providerDir, client)
	}
}

func handle(c *gin.Context, providerDir string, client *resty.Client) {
	providerName := c.Param("provider")
	if !filestore.SafeName(providerName) {
		c.String(403, "Forbidden")
		return
	}

	if providerName == proxyDNSProviderName {
		handleProxyDNS(c, providerDir, client)
		return
	}

	result, status, err := fetchProviderResult(providerDir, providerName, client)
	if err != nil {
		zap.S().Errorw("failed to fetch provider subscription", "provider", providerName, "status", status)
		if status == 404 {
			c.String(404, "Not found")
			return
		}
		c.String(403, "Forbidden")
		return
	}

	writeSubscription(c, result)
}

func writeSubscription(c *gin.Context, result *contentResult) {
	if result.subscriptionUserinfo != "" {
		c.Header("subscription-userinfo", result.subscriptionUserinfo)
	}
	c.Data(200, "text/plain; charset=UTF-8", result.body)
}
