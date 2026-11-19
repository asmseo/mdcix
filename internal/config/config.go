// Package config loads mdcix settings from flags, env, and optional YAML.
package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the runtime configuration for serve / fetch.
type Config struct {
	Listen   string        `yaml:"listen"`
	Mode     string        `yaml:"mode"` // reader | agent
	Encoding string        `yaml:"encoding"`
	MITM     bool          `yaml:"mitm"`
	CertDir  string        `yaml:"cert_dir"`
	Insecure bool          `yaml:"insecure"`
	Timeout  time.Duration `yaml:"timeout"`
	MaxBody  int64         `yaml:"max_body"`
	UserAgent string       `yaml:"user_agent"`
}

// Default returns sensible MVP defaults.
func Default() Config {
	return Config{
		Listen:    ":8080",
		Mode:      "reader",
		Encoding:  "cl100k_base",
		MITM:      true,
		CertDir:   "./certs",
		Insecure:  false,
		Timeout:   30 * time.Second,
		MaxBody:   8 << 20, // 8 MiB
		UserAgent: "mdcix/0.1 (+https://github.com/asmseo/mdcix)",
	}
}

// LoadYAML merges a YAML file into base. Missing file is OK when path is empty.
func LoadYAML(path string, base Config) (Config, error) {
	if path == "" {
		return base, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return base, fmt.Errorf("read config: %w", err)
	}
	var overlay Config
	if err := yaml.Unmarshal(data, &overlay); err != nil {
		return base, fmt.Errorf("parse config: %w", err)
	}
	if overlay.Listen != "" {
		base.Listen = overlay.Listen
	}
	if overlay.Mode != "" {
		base.Mode = overlay.Mode
	}
	if overlay.Encoding != "" {
		base.Encoding = overlay.Encoding
	}
	if overlay.CertDir != "" {
		base.CertDir = overlay.CertDir
	}
	if overlay.UserAgent != "" {
		base.UserAgent = overlay.UserAgent
	}
	if overlay.Timeout > 0 {
		base.Timeout = overlay.Timeout
	}
	if overlay.MaxBody > 0 {
		base.MaxBody = overlay.MaxBody
	}
	// Bools: only override when key present is hard in yaml.v3 without custom types;
	// accept explicit true/false from file by checking raw map for mitm/insecure.
	var raw map[string]any
	_ = yaml.Unmarshal(data, &raw)
	if _, ok := raw["mitm"]; ok {
		base.MITM = overlay.MITM
	}
	if _, ok := raw["insecure"]; ok {
		base.Insecure = overlay.Insecure
	}
	return base, nil
}
