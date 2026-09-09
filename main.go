package main

import (
	"flag"
	"os"
	"path/filepath"
	"time"

	"github.com/alecthw/sub-server/handler"
	"github.com/alecthw/sub-server/log"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

var (
	workDir             string
	host                string
	subconvUrl          string
	managedConfigPrefix string
)

func init() {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))

	flag.StringVar(&workDir, "dir", dir, "The wroking directory. Default executable file directory.")
	flag.StringVar(&host, "host", ":8080", "Http server bind host. Default \":8080\"")
	flag.StringVar(&subconvUrl, "subcnv", "http://127.0.0.1:25500", "subconverter server bind host. Default \"http://127.0.0.1:25500\"")
	flag.StringVar(&managedConfigPrefix, "mcp", "", "Set MANAGED-CONFIG for surge and surfboard. If emty, MANAGED-CONFIG will not be set. Default \"\"")

	flag.Parse()

	gin.SetMode("release")
}

func main() {
	client := resty.New()
	server := handler.New(handler.Config{WorkDir: workDir, SubconverterURL: subconvUrl, ManagedConfigPrefix: managedConfigPrefix}, client)
	server.StartProviderDNSPolicyScheduler()

	r := gin.New()

	r.Use(ginzap.Ginzap(log.Logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log.Logger, true))

	zap.S().Infow("main")

	server.RegisterRoutes(r)

	if err := r.Run(host); err != nil {
		zap.S().Fatalw("server stopped", "error", err)
	}
}
