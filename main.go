package main

import (
	"flag"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthw/sub-server/log"
	"gopkg.in/ini.v1"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

var (
	workDir    string
	host       string
	subconvUrl string

	client *resty.Client
)

func init() {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))

	flag.StringVar(&workDir, "dir", dir, "The wroking directory. Default executable file directory.")
	flag.StringVar(&host, "host", ":8080", "Http server bind host. Default \":8080\"")
	flag.StringVar(&subconvUrl, "subcnv", "http://127.0.0.1:25500", "subconverter server bind host. Default \"http://127.0.0.1:25500\"")

	flag.Parse()

	gin.SetMode("release")

	client = resty.New()
}

func main() {
	r := gin.New()

	r.Use(ginzap.Ginzap(log.Logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log.Logger, true))

	zap.S().Infow("main")

	r.GET("/:uuid/:file", func(c *gin.Context) {
		uuid := c.Param("uuid")
		file := c.Param("file")

		// check uuid and file name valid
		if !isValidUUID(uuid) || !isPathSecure(file) {
			c.String(403, "Forbidden")
			return
		}

		// check user path exist
		userPath := filepath.Join(workDir, "sub", uuid)
		if !pathExists(userPath) {
			c.String(404, "Not found")
			return
		}

		// check file exist
		subFilePath := filepath.Join(userPath, file)
		if !pathExists(subFilePath) {
			c.String(404, "Not found")
			return
		}

		// get file content
		fileContent, subFilePath, err := getFileContent(uuid, file)
		if err != nil {
			c.String(404, "Not found")
			return
		}

		// if file is ini, redirect to subconverter
		fileExt := path.Ext(subFilePath)
		if fileExt == ".ini" {
			c.Data(200, "text/plain; charset=UTF-8", getSubconv(fileContent))
			return
		}

		c.Data(200, "text/plain; charset=UTF-8", fileContent)
	})

	r.Run(host)
}

// get file content and check if it is a redirect file
func getFileContent(uuid string, file string) ([]byte, string, error) {
	subFilePath := filepath.Join(workDir, "sub", uuid, file)
	fileContent, err := os.ReadFile(subFilePath)
	if err != nil {
		return nil, subFilePath, err
	}

	fileStr := string(fileContent)
	if strings.HasPrefix(fileStr, "[Redirect]") {
		cfgs, _ := ini.Load((fileContent))
		nextFile := cfgs.Section("Redirect").Key("file").String()
		nextUuid := uuid
		if cfgs.Section("Redirect").HasKey("uuid") {
			nextUuid = cfgs.Section("Redirect").Key("uuid").String()
		}
		fileContent, subFilePath, _ = getFileContent(nextUuid, nextFile)
		return fileContent, subFilePath, nil
	}

	return fileContent, subFilePath, nil
}

// get subconverter response
func getSubconv(fileContent []byte) []byte {
	cfgs, err := ini.Load((fileContent))
	if err != nil {
		return []byte("")
	}

	resp, err := client.R().
		SetQueryParams(cfgs.Section("Profile").KeysHash()).
		Get(subconvUrl + "/sub")

	if err != nil {
		return []byte("")
	}

	return resp.Body()
}

// check if a string is a valid uuid
func isValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

// check if a path is secure
func isPathSecure(filePath string) bool {
	if strings.Contains(filePath, "..") || strings.Contains(filePath, "/") || strings.Contains(filePath, "\\") {
		return false
	} else {
		return true
	}
}

// check if a path exists
func pathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}
