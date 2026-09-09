// Package handler wires HTTP routes to subscription services.
package handler

import (
	"path/filepath"
	"strings"

	"github.com/alecthw/sub-server/internal/filestore"
	"github.com/alecthw/sub-server/internal/provider"
	"github.com/alecthw/sub-server/internal/subscription"
	templateinject "github.com/alecthw/sub-server/internal/template"
	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

type Config struct {
	WorkDir             string
	SubconverterURL     string
	ManagedConfigPrefix string
}

// SubscriptionStore separates file access policy from HTTP response handling.
type SubscriptionStore interface {
	Resolve(uid, file string) (filestore.File, error)
	LoadEntries(uid string) ([]subscription.Entry, error)
}

// Server owns immutable configuration and reusable dependencies per instance.
type Server struct {
	store              SubscriptionStore
	subconvURL         string
	managedPrefix      string
	providerDir        string
	client             *resty.Client
	templates          *templateinject.Registry
	loadProxyDNSPolicy templateinject.ProxyDNSPolicyLoader
	providerHandler    gin.HandlerFunc
}

func New(config Config, client *resty.Client) *Server {
	if client == nil {
		client = resty.New()
	}
	providerDir := filepath.Join(config.WorkDir, "sub", "provider")
	return &Server{
		store:              filestore.New(filepath.Join(config.WorkDir, "sub")),
		subconvURL:         strings.TrimRight(config.SubconverterURL, "/"),
		managedPrefix:      strings.TrimRight(config.ManagedConfigPrefix, "/"),
		providerDir:        providerDir,
		client:             client,
		templates:          templateinject.DefaultRegistry(),
		loadProxyDNSPolicy: func() ([]byte, error) { return provider.LoadOrGenerateProxyDNSPolicy(providerDir, client) },
		providerHandler:    provider.Handler(providerDir, client),
	}
}

// RegisterRoutes is shared by the executable and HTTP integration tests.
func (s *Server) RegisterRoutes(routes gin.IRoutes) {
	routes.GET("/provider/:provider", s.providerHandler)
	routes.GET("/:uuid/:file", s.SubscribeHandler)
}
