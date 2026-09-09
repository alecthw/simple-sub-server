// Package filestore owns subscription file access and access policy.
package filestore

import (
	"bufio"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthw/sub-server/internal/subscription"
	"github.com/google/uuid"
)

// Source identifies whether a file is a user override or shared configuration.
type Source uint8

const (
	User Source = iota
	Template
	Converter
)

type File struct {
	Content []byte
	Source  Source
}

// Store reads files beneath subDir. It has no request-specific mutable state.
type Store struct{ subDir string }

func New(subDir string) *Store { return &Store{subDir: subDir} }

// SafeName accepts a single filename, never a parent or child path.
func SafeName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "/\\\x00") && !strings.Contains(name, "..")
}

func Readable(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".conf", ".json", ".ini":
		return true
	default:
		return false
	}
}

// Resolve enforces access policy and falls back only when the user file is absent.
// Root-scoped reads also prevent symlinks escaping the selected directory.
func (s *Store) Resolve(uid, name string) (File, error) {
	if _, err := uuid.Parse(uid); err != nil || !SafeName(name) || !Readable(name) {
		return File{}, fs.ErrPermission
	}
	user, err := s.openUser(uid)
	if err != nil {
		return File{}, err
	}
	defer user.Close()
	if err := checkWhitelist(user, name); err != nil {
		return File{}, err
	}
	content, err := readFile(user, name)
	if err == nil {
		return File{Content: content, Source: User}, nil
	}
	if !os.IsNotExist(err) {
		return File{}, err
	}
	directory, source := "template", Template
	if filepath.Ext(name) == ".ini" {
		directory, source = "subconv", Converter
	}
	root, err := os.OpenRoot(filepath.Join(s.subDir, directory))
	if err != nil {
		return File{}, err
	}
	defer root.Close()
	content, err = readFile(root, name)
	return File{Content: content, Source: source}, err
}

func (s *Store) LoadEntries(uid string) ([]subscription.Entry, error) {
	user, err := s.openUser(uid)
	if err != nil {
		return nil, err
	}
	defer user.Close()
	file, err := user.Open("subscribe.txt")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return subscription.Parse(file)
}

func (s *Store) openUser(uid string) (*os.Root, error) {
	if _, err := uuid.Parse(uid); err != nil {
		return nil, fs.ErrPermission
	}
	root, err := os.OpenRoot(s.subDir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenRoot(uid)
}

func readFile(root *os.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(file)
}

func checkWhitelist(root *os.Root, name string) error {
	file, err := root.Open("whitelist.txt")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fs.ErrPermission
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	allowed := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if line == name {
			allowed = true
		}
	}
	if scanner.Err() != nil || !allowed {
		return fs.ErrPermission
	}
	return nil
}
