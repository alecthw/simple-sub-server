package handler

import (
	"bufio"
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gopkg.in/ini.v1"
)

// SubscribeHandler handles GET /:uuid/:file
func SubscribeHandler(c *gin.Context) {
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

	// if file is ini, send it to subconverter
	fileExt := path.Ext(subFilePath)
	if fileExt == ".ini" {
		c.Data(200, "text/plain; charset=UTF-8", getSubconv(uid, mcUrl, fileContent))
		return
	}

	c.Data(200, "text/plain; charset=UTF-8", fileContent)
}

// get file content
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
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			urls = append(urls, trimmed)
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
