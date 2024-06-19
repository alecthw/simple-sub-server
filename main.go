package main

import (
	"bufio"
	"bytes"
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
	workDir             string
	host                string
	subconvUrl          string
	managedConfigPrefix string

	subDir string

	client *resty.Client
)

func init() {
	dir, _ := filepath.Abs(filepath.Dir(os.Args[0]))

	flag.StringVar(&workDir, "dir", dir, "The wroking directory. Default executable file directory.")
	flag.StringVar(&host, "host", ":8080", "Http server bind host. Default \":8080\"")
	flag.StringVar(&subconvUrl, "subcnv", "http://127.0.0.1:25500", "subconverter server bind host. Default \"http://127.0.0.1:25500\"")
	flag.StringVar(&managedConfigPrefix, "mcp", "", "Set MANAGED-CONFIG for surge and surfboard. If emty, MANAGED-CONFIG will not be set. Default \"\"")

	flag.Parse()

	subDir = filepath.Join(workDir, "sub")

	gin.SetMode("release")

	client = resty.New()
}

func main() {
	r := gin.New()

	r.Use(ginzap.Ginzap(log.Logger, time.RFC3339, true))
	r.Use(ginzap.RecoveryWithZap(log.Logger, true))

	zap.S().Infow("main")

	r.GET("/:uuid/:file", func(c *gin.Context) {
		mcUrl := managedConfigPrefix + c.Request.RequestURI

		uid := c.Param("uuid")
		file := c.Param("file")

		// check uuid and file name valid
		if !isValidUUID(uid) || !isPathSecure(file) {
			c.String(403, "Forbidden")
			return
		}

		// check user path exist
		userPath := filepath.Join(subDir, uid)
		if !pathExists(userPath) {
			c.String(404, "Not found")
			return
		}

		// check file exist
		subFilePath := filepath.Join(userPath, file)
		if !pathExists(subFilePath) {
			tplFilePath := filepath.Join(subDir, "template", file)
			urlFilePath := filepath.Join(userPath, "subscribe.txt")
			if (!pathExists(tplFilePath)) || (!pathExists(urlFilePath)) {
				c.String(404, "Not found")
				return
			}
		}

		// get file content
		fileContent, subFilePath, err := getFileContent(uid, file)
		if err != nil {
			c.String(404, "Not found")
			return
		}

		// if file is ini, redirect to subconverter
		fileExt := path.Ext(subFilePath)
		if fileExt == ".ini" {
			c.Data(200, "text/plain; charset=UTF-8", getSubconv(uid, mcUrl, fileContent))
			return
		}

		c.Data(200, "text/plain; charset=UTF-8", fileContent)
	})

	_ = r.Run(host)
}

// get file content and check if it is a redirect file
func getFileContent(uid string, file string) ([]byte, string, error) {
	subFilePath := filepath.Join(subDir, uid, file)
	fileContent, err := os.ReadFile(subFilePath)
	if err != nil {
		// try to get file from template
		if uid != "template" {
			fileContent, subFilePath, err = getFileContent("template", file)
			return fileContent, subFilePath, nil
		}
		return nil, subFilePath, err
	}

	fileStr := string(fileContent)
	if strings.HasPrefix(fileStr, "[Redirect]") {
		cfgs, _ := ini.Load(fileContent)
		nextFile := cfgs.Section("Redirect").Key("file").String()
		nextUid := uid
		if cfgs.Section("Redirect").HasKey("uuid") {
			nextUid = cfgs.Section("Redirect").Key("uuid").String()
		}
		fileContent, subFilePath, err = getFileContent(nextUid, nextFile)
		if err != nil {
			return nil, subFilePath, err
		}
		return fileContent, subFilePath, nil
	}

	return fileContent, subFilePath, nil
}

// get subscribe urls
func getSubscribeUrls(uid string) (string, error) {
	urlFilePath := filepath.Join(subDir, uid, "subscribe.txt")

	fh, err := os.Open(urlFilePath)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = fh.Close()
	}()

	scanner := bufio.NewScanner(fh)
	scanner.Split(bufio.ScanLines)

	var urls []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			urls = append(urls, scanner.Text())
		}
	}

	return strings.Join(urls, "|"), nil
}

// get subconverter response
func getSubconv(uid string, mcUrl string, fileContent []byte) []byte {
	cfgs, err := ini.Load(fileContent)
	if err != nil {
		return []byte("")
	}

	if !cfgs.Section("Profile").HasKey("url") {
		urlStr, err := getSubscribeUrls(uid)
		if err != nil {
			return []byte("")
		}

		_, _ = cfgs.Section("Profile").NewKey("url", urlStr)
	}

	resp, err := client.R().
		SetQueryParams(cfgs.Section("Profile").KeysHash()).
		Get(subconvUrl + "/sub")

	if err != nil {
		return []byte("")
	}

	target := cfgs.Section("Profile").Key("target").String()
	if managedConfigPrefix != "" && (target == "surge" || target == "surfboard") {
		var buffer bytes.Buffer
		buffer.Write([]byte("#!MANAGED-CONFIG " + mcUrl + " interval=43200 strict=true\n"))
		buffer.Write(resp.Body())
		return buffer.Bytes()
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
