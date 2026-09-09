package provider

import (
	"os"
	"path/filepath"

	"github.com/alecthw/sub-server/internal/filestore"
	"gopkg.in/yaml.v3"
)

// Config represents a provider YAML configuration file.
type Config struct {
	CfgUrls      []string          `yaml:"cfgUrls"`
	Username     string            `yaml:"username"`
	Password     string            `yaml:"password"`
	Headers      map[string]string `yaml:"headers"`
	Decrypt      *DecryptConfig    `yaml:"decrypt"`
	SubscribeUrl string            `yaml:"subscribeUrl"`
}

// DecryptConfig enables the xjkp subscription payload decryption.
type DecryptConfig struct {
	Key string `yaml:"key"`
	IV  string `yaml:"iv"`
}

func loadConfig(providerDir string, provider string) (*Config, error) {
	if !filestore.SafeName(provider) {
		return nil, os.ErrPermission
	}
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func saveConfig(providerDir string, provider string, config *Config) error {
	if !filestore.SafeName(provider) {
		return os.ErrPermission
	}
	configPath := filepath.Join(providerDir, provider+".yml")
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}
