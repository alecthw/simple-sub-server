package handler

import (
	"bufio"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/alecthw/sub-server/handler/subconv"
	"github.com/alecthw/sub-server/handler/subscription"
	templateinject "github.com/alecthw/sub-server/handler/template"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SubscribeHandler handles GET /:uuid/:file.
func SubscribeHandler(c *gin.Context) {
	uid := c.Param("uuid")
	file := c.Param("file")

	if !isValidUUID(uid) || !isPathSecure(file) || !isURLReadableSubscriptionFile(file) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}

	userPath := filepath.Join(subDir, uid)
	if !pathExists(userPath) {
		c.String(http.StatusNotFound, "Not found")
		return
	}
	if !isFileAllowedByWhitelist(userPath, file) {
		c.String(http.StatusForbidden, "Forbidden")
		return
	}

	subFilePath := filepath.Join(userPath, file)
	if !pathExists(subFilePath) {
		fallbackFilePath := getFallbackFilePath(file)
		if !pathExists(fallbackFilePath) {
			c.String(http.StatusNotFound, "Not found")
			return
		}

		if path.Ext(fallbackFilePath) == ".ini" || templateinject.IsSubscribable(file) {
			if !pathExists(filepath.Join(userPath, "subscribe.txt")) {
				c.String(http.StatusNotFound, "Not found")
				return
			}
		}
	}

	fileContent, filePath, err := getFileContent(uid, file)
	if err != nil {
		c.String(http.StatusNotFound, "Not found")
		return
	}

	if path.Ext(filePath) == ".ini" {
		respondINI(c, uid, file, fileContent)
		return
	}

	fileContent, err = appendTemplateContent(uid, file, filePath, fileContent)
	if err != nil {
		if os.IsNotExist(err) {
			c.String(http.StatusNotFound, "Not found")
			return
		}
		c.String(http.StatusInternalServerError, "Internal server error")
		return
	}

	c.Data(http.StatusOK, "text/plain; charset=UTF-8", fileContent)
}

func respondINI(c *gin.Context, uid string, file string, fileContent []byte) {
	entries, err := subscription.LoadEntries(subDir, uid)
	if err != nil {
		c.String(http.StatusNotFound, "Not found")
		return
	}

	resp, err := subconv.Handle(subconv.Context{
		UID:              uid,
		File:             file,
		ManagedConfigURL: managedConfigPrefix + c.Request.RequestURI,
		SubconverterURL:  subconvUrl,
		Client:           client,
		Entries:          entries,
		RedirectURLForFile: func(nextFile string) string {
			return getRedirectURL(uid, nextFile)
		},
	}, fileContent)
	if err != nil {
		c.String(http.StatusNotFound, "Not found")
		return
	}

	if resp.Location != "" {
		c.Redirect(resp.Status, resp.Location)
		return
	}
	c.Data(resp.Status, resp.ContentType, resp.Body)
}

func getRedirectURL(uid string, file string) string {
	if managedConfigPrefix == "" {
		return file
	}
	return strings.TrimRight(managedConfigPrefix, "/") + "/" + uid + "/" + file
}

func getFileContent(uid string, file string) ([]byte, string, error) {
	subFilePath := filepath.Join(subDir, uid, file)
	fileContent, err := os.ReadFile(subFilePath)
	if err == nil {
		return fileContent, subFilePath, nil
	}

	fallbackFilePath := getFallbackFilePath(file)
	fileContent, err = os.ReadFile(fallbackFilePath)
	if err != nil {
		return nil, fallbackFilePath, err
	}

	return fileContent, fallbackFilePath, nil
}

func getFallbackFilePath(file string) string {
	if path.Ext(file) == ".ini" {
		return filepath.Join(subDir, "subconv", file)
	}

	return filepath.Join(subDir, "template", file)
}

func appendTemplateContent(uid string, file string, filePath string, fileContent []byte) ([]byte, error) {
	if !isTemplateFilePath(filePath) {
		return fileContent, nil
	}
	if !templateinject.IsSubscribable(file) {
		return nil, os.ErrNotExist
	}

	entries, err := subscription.LoadEntries(subDir, uid)
	if err != nil {
		return nil, err
	}

	return templateinject.Inject(templateinject.Context{
		UID:        uid,
		File:       file,
		ManagedURL: getManagedConfigURL(uid, file),
		Entries:    entries,
	}, fileContent)
}

func isTemplateFilePath(filePath string) bool {
	return filepath.Dir(filePath) == filepath.Join(subDir, "template")
}

func getManagedConfigURL(uid string, file string) string {
	if managedConfigPrefix == "" {
		return ""
	}
	return managedConfigPrefix + "/" + uid + "/" + file
}

func isValidUUID(u string) bool {
	_, err := uuid.Parse(u)
	return err == nil
}

func isPathSecure(filePath string) bool {
	return !strings.Contains(filePath, "..") && !strings.Contains(filePath, "/") && !strings.Contains(filePath, "\\")
}

func isURLReadableSubscriptionFile(file string) bool {
	switch strings.ToLower(path.Ext(file)) {
	case ".yaml", ".yml", ".conf", ".json", ".ini":
		return true
	default:
		return false
	}
}

func isFileAllowedByWhitelist(userPath string, file string) bool {
	whitelistPath := filepath.Join(userPath, "whitelist.txt")
	fh, err := os.Open(whitelistPath)
	if err != nil {
		return os.IsNotExist(err)
	}
	defer func() {
		_ = fh.Close()
	}()

	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		allowedFile := strings.TrimSpace(scanner.Text())
		if allowedFile == "" || strings.HasPrefix(allowedFile, "#") {
			continue
		}
		if allowedFile == file {
			return true
		}
	}
	return false
}

func pathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	if err != nil {
		return !os.IsNotExist(err)
	}
	return true
}
