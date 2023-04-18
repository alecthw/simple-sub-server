package main

import (
	"flag"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alecthw/sub-server/log"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var (
	cfgName string

	workDir string
	config  *Config
)

func init() {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	flag.StringVar(&workDir, "dir", dir, "The wroking directory. Default executable file directory.")
	flag.StringVar(&cfgName, "cfg", "cfg.json", "The configuration file name. Default \"cfg.json\"")
	flag.Parse()

	gin.SetMode("release")
}

func main() {
	config = LoadConfig(filepath.Join(workDir, cfgName))

	r := gin.New()

	r.Use(ginzap.Ginzap(log.Logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log.Logger, true))

	zap.S().Infow("main")

	r.GET("/:uuid", func(c *gin.Context) {
		uuid := c.Param("uuid")
		subFilePath, ok := config.Files[uuid]
		if ok {
			fileContent, err := os.ReadFile(filepath.Join(workDir, "sub", subFilePath))
			if err != nil {
				c.String(404, "Not found")
				return
			}

			filePrefix := strings.TrimSuffix(path.Base(subFilePath), path.Ext(subFilePath))
			c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+filePrefix)

			c.Data(200, "text/plain; charset=UTF-8", fileContent)
		} else {
			c.String(404, "Not found")
		}
	})

	r.Run(config.Address + ":" + strconv.Itoa(config.Port))
}
