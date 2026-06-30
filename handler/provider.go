package handler

import (
	providerhandler "github.com/alecthw/sub-server/handler/provider"
	"github.com/gin-gonic/gin"
)

// ProviderHandler handles GET /provider/:provider.
func ProviderHandler(c *gin.Context) {
	providerhandler.Handler(providerDir, client)(c)
}
