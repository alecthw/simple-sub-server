package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthw/sub-server/log"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	workDir string
	host    string
)

func init() {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))
	flag.StringVar(&workDir, "dir", dir, "The wroking directory. Default executable file directory.")

	flag.StringVar(&host, "host", ":8080", "Http server bind host. Default \":8080\"")
	flag.Parse()

	gin.SetMode("release")
}

func main() {
	r := gin.New()

	r.Use(ginzap.Ginzap(log.Logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log.Logger, true))

	zap.S().Infow("main")

	r.GET("/:uuid/:file", func(c *gin.Context) {
		uuid := c.Param("uuid")
		file := c.Param("file")

		if !isValidUUID(uuid) || !isPathSecure(file) {
			c.String(403, "Forbidden")
			return
		}

		userPath := filepath.Join(workDir, "sub", uuid)
		if !pathExists(userPath) {
			c.String(404, "Not found")
			return
		}

		subFilePath := filepath.Join(userPath, file)
		if !pathExists(subFilePath) {
			c.String(404, "Not found")
			return
		}

		fileContent, err := os.ReadFile(subFilePath)
		if err != nil {
			c.String(404, "Not found")
			return
		}

		c.Data(200, "text/plain; charset=UTF-8", fileContent)
	})

	r.Run(host)
}

func isValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

func isPathSecure(filePath string) bool {
	if strings.Contains(filePath, "..") || strings.Contains(filePath, "/") || strings.Contains(filePath, "\\") {
		return false
	} else {
		return true
	}
}

func pathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}
