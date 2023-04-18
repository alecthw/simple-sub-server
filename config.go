package main

import (
	"encoding/json"
	"os"

	"go.uber.org/zap"
)

type Config struct {
	Address string            `ini:"address"`
	Port    int               `ini:"port"`
	Files   map[string]string `ini:"files"`
}

func LoadConfig(file string) *Config {
	content, err := os.ReadFile(file)
	if err != nil {
		zap.S().Errorw("Error when opening file: ", err)
	}

	// Now let's unmarshall the data into `payload`
	var cfg Config
	err = json.Unmarshal(content, &cfg)
	if err != nil {
		zap.S().Errorw("Error during Unmarshal(): ", err)
	}

	return &cfg
}
