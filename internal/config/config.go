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
	Listen           string        `yaml:"listen"`
	Mode             string        `yaml:"mode"` // reader | agent
	Encoding         string        `yaml:"encoding"`
	MITM             bool          `yaml:"mitm"`
	CertDir          string        `yaml:"cert_dir"`
	Insecure         bool          `yaml:"insecure"`
	Timeout          time.Duration `yaml:"timeout"`
	MaxBody          int64         `yaml:"max_body"`
	UserAgent        string        `yaml:"user_agent"`
	CacheDir         string        `yaml:"cache_dir"`
	CacheTTL         time.Duration `yaml:"cache_ttl"`
	MaxCacheEntries  int           `yaml:"max_cache_entries"`
	AllowPrivate     bool          `yaml:"allow_private"`
	Telemetry        bool          `yaml:"telemetry"` // default false
	SkipRobots       bool          `yaml:"skip_robots"`
	PolicyEnabled    bool          `yaml:"policy_enabled"`
}

// Default returns sensible MVP defaults.
func Default() Config {
	return Config{
		Listen:          ":8080",
		Mode:            "reader",
		Encoding:        "cl100k_base",
		MITM:            true,
		CertDir:         "./certs",
		Insecure:        false,
		Timeout:         30 * time.Second,
		MaxBody:         8 << 20, // 8 MiB
		UserAgent:       "mdcix/0.2 (+https://github.com/asmseo/mdcix)",
		CacheDir:        "",
		CacheTTL:        24 * time.Hour,
		MaxCacheEntries: 0,
		Telemetry:       false,
		PolicyEnabled:   true,
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
	if overlay.CacheDir != "" {
		base.CacheDir = overlay.CacheDir
	}
	if overlay.CacheTTL > 0 {
		base.CacheTTL = overlay.CacheTTL
	}
	if overlay.MaxCacheEntries > 0 {
		base.MaxCacheEntries = overlay.MaxCacheEntries
	}
	var raw map[string]any
	_ = yaml.Unmarshal(data, &raw)
	if _, ok := raw["mitm"]; ok {
		base.MITM = overlay.MITM
	}
	if _, ok := raw["insecure"]; ok {
		base.Insecure = overlay.Insecure
	}
	if _, ok := raw["allow_private"]; ok {
		base.AllowPrivate = overlay.AllowPrivate
	}
	if _, ok := raw["telemetry"]; ok {
		base.Telemetry = overlay.Telemetry
	}
	if _, ok := raw["skip_robots"]; ok {
		base.SkipRobots = overlay.SkipRobots
	}
	if _, ok := raw["policy_enabled"]; ok {
		base.PolicyEnabled = overlay.PolicyEnabled
	}
	return base, nil
}
