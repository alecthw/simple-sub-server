package handler

import (
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/alecthw/sub-server/internal/filestore"
	"github.com/alecthw/sub-server/internal/subconv"
	templateinject "github.com/alecthw/sub-server/internal/template"
	"github.com/gin-gonic/gin"
)

// SubscribeHandler handles GET /:uuid/:file.
func (s *Server) SubscribeHandler(c *gin.Context) {
	uid, file := c.Param("uuid"), c.Param("file")
	resolved, err := s.store.Resolve(uid, file)
	if err != nil {
		respondError(c, err)
		return
	}
	if filepath.Ext(file) == ".ini" {
		s.respondINI(c, uid, file, resolved.Content)
		return
	}
	content := resolved.Content
	if resolved.Source == filestore.Template {
		injector := s.templates.Find(file)
		if injector == nil {
			respondError(c, fs.ErrNotExist)
			return
		}
		entries, err := s.store.LoadEntries(uid)
		if err != nil {
			respondError(c, err)
			return
		}
		content, err = injector.Inject(templateinject.Context{
			UID: uid, File: file, ManagedURL: s.managedURL(uid, file),
			Entries: entries, LoadProxyDNSPolicy: s.loadProxyDNSPolicy,
		}, content)
		if err != nil {
			respondError(c, err)
			return
		}
	}
	c.Data(http.StatusOK, "text/plain; charset=UTF-8", content)
}

func (s *Server) respondINI(c *gin.Context, uid, file string, content []byte) {
	entries, err := s.store.LoadEntries(uid)
	if err != nil {
		respondError(c, err)
		return
	}
	managedURL := ""
	if s.managedPrefix != "" {
		managedURL = s.managedPrefix + c.Request.URL.RequestURI()
	}
	response, err := subconv.Handle(subconv.Context{
		ManagedConfigURL: managedURL, SubconverterURL: s.subconvURL,
		Client: s.client, RequestContext: c.Request.Context(), Entries: entries,
		RedirectURLForFile: func(nextFile string) string {
			if s.managedPrefix == "" {
				return url.PathEscape(nextFile)
			}
			return s.managedURL(uid, nextFile)
		},
	}, content)
	if err != nil {
		if errors.Is(err, subconv.ErrUpstream) {
			c.String(http.StatusBadGateway, "Bad gateway")
			return
		}
		c.String(http.StatusNotFound, "Not found")
		return
	}
	if response.Location != "" {
		c.Redirect(response.Status, response.Location)
		return
	}
	c.Data(response.Status, response.ContentType, response.Body)
}

func (s *Server) managedURL(uid, file string) string {
	if s.managedPrefix == "" {
		return ""
	}
	return s.managedPrefix + "/" + url.PathEscape(uid) + "/" + url.PathEscape(file)
}

func respondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, fs.ErrPermission):
		c.String(http.StatusForbidden, "Forbidden")
	case errors.Is(err, fs.ErrNotExist):
		c.String(http.StatusNotFound, "Not found")
	default:
		c.String(http.StatusInternalServerError, "Internal server error")
	}
}
