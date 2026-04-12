package handler

import (
	"path/filepath"

	"github.com/go-resty/resty/v2"
)

var (
	subDir              string
	subconvUrl          string
	managedConfigPrefix string
	providerDir         string
	client              *resty.Client
)

// Init initializes the handler package with shared configuration
func Init(workDir string, subcnv string, mcp string, c *resty.Client) {
	subDir = filepath.Join(workDir, "sub")
	subconvUrl = subcnv
	managedConfigPrefix = mcp
	providerDir = filepath.Join(subDir, "provider")
	client = c
}
