package template

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/alecthw/sub-server/handler/subscription"
)

// Context carries runtime data used by template injectors.
type Context struct {
	UID        string
	File       string
	ManagedURL string
	Entries    []subscription.Entry
}

// Injector injects subscription information into a template format.
type Injector interface {
	Match(file string) bool
	Inject(ctx Context, content []byte) ([]byte, error)
}

var injectors = []Injector{
	ClashInjector{},
	StashInjector{},
	EgernInjector{},
	SurgeInjector{},
	LoonInjector{},
	QuanxInjector{},
}

// IsSubscribable reports whether the template may need subscribe.txt.
func IsSubscribable(file string) bool {
	return findInjector(file) != nil
}

// Inject applies the matching template injector.
func Inject(ctx Context, content []byte) ([]byte, error) {
	injector := findInjector(ctx.File)
	if injector == nil {
		return content, nil
	}
	return injector.Inject(ctx, content)
}

func findInjector(file string) Injector {
	for _, injector := range injectors {
		if injector.Match(file) {
			return injector
		}
	}
	return nil
}

func isNamedConfFile(file string, prefix string) bool {
	lowerFile := strings.ToLower(filepath.Base(file))
	return strings.HasPrefix(lowerFile, prefix) && path.Ext(lowerFile) == ".conf"
}

func isNamedYamlFile(file string, prefix string) bool {
	lowerFile := strings.ToLower(filepath.Base(file))
	ext := path.Ext(lowerFile)
	return strings.HasPrefix(lowerFile, prefix) && (ext == ".yaml" || ext == ".yml")
}
