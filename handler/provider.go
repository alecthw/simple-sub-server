package handler

import (
	providerhandler "github.com/alecthw/sub-server/handler/provider"
	"github.com/gin-gonic/gin"
)

// ProviderHandler handles GET /provider/:provider.
func ProviderHandler(c *gin.Context) {
	providerhandler.Handler(providerDir, client)(c)
}

// StartProviderDNSPolicyScheduler starts the daily proxy-dns.yml refresh.
func StartProviderDNSPolicyScheduler() {
	providerhandler.StartProxyDNSPolicyScheduler(providerDir, client)
}
