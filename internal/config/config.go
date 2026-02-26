package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Config holds all configuration loaded from config.yaml.
type Config struct {
	ScanPaths          []string    `yaml:"scan_paths"           json:"scan_paths"`
	ExcludePaths       []string    `yaml:"exclude_paths"        json:"exclude_paths"`
	Schedule           string      `yaml:"schedule"             json:"schedule"`
	ScanPaused         bool        `yaml:"scan_paused"          json:"scan_paused"`
	TrashDir           string      `yaml:"trash_dir"            json:"-"`
	TrashRetentionDays int         `yaml:"trash_retention_days" json:"trash_retention_days"`
	DBPath             string      `yaml:"db_path"              json:"-"`
	HTTPAddr           string      `yaml:"http_addr"            json:"-"`
	ScanWorkers        ScanWorkers `yaml:"scan_workers"         json:"scan_workers"`
	LogLevel           string      `yaml:"log_level"            json:"-"`
}

// ScanWorkers holds concurrency knobs for the scan pipeline.
type ScanWorkers struct {
	Walkers        int `yaml:"walkers"         json:"walkers"`
	CacheCheckers  int `yaml:"cache_checkers"  json:"cache_checkers"`
	PartialHashers int `yaml:"partial_hashers" json:"partial_hashers"`
	FullHashers    int `yaml:"full_hashers"    json:"full_hashers"`
}

// applyDefaults fills zero/empty fields with sensible defaults.
func (c *Config) applyDefaults() {
	if c.Schedule == "" {
		c.Schedule = "0 2 * * 0"
	}
	if c.TrashDir == "" {
		c.TrashDir = "/data/trash"
	}
	if c.TrashRetentionDays == 0 {
		c.TrashRetentionDays = 30
	}
	if c.DBPath == "" {
		c.DBPath = "/data/ditto.db"
	}
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.ScanWorkers.Walkers == 0 {
		c.ScanWorkers.Walkers = 4
	}
	if c.ScanWorkers.CacheCheckers == 0 {
		c.ScanWorkers.CacheCheckers = 4
	}
	if c.ScanWorkers.PartialHashers == 0 {
		c.ScanWorkers.PartialHashers = 4
	}
	if c.ScanWorkers.FullHashers == 0 {
		c.ScanWorkers.FullHashers = 2
	}
	if c.LogLevel == "" {
		c.LogLevel = "info"
	}
}

// Load reads and parses the YAML config file at path.
// If the file does not exist, Load returns a default Config so the server
// can start without a mounted config file (useful for bare Docker runs).
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		var cfg Config
		cfg.applyDefaults()
		return &cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}

// MergeDBSettings overlays settings stored in the DB on top of the config.
// Keys recognised: "scan_paths", "exclude_paths", "schedule", "scan_paused",
// "trash_retention_days", "walkers", "partial_hashers", "full_hashers".
// Unknown keys and parse errors are silently ignored.
func MergeDBSettings(cfg *Config, settings map[string]string) {
	if v, ok := settings["scan_paths"]; ok && v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err == nil {
			cfg.ScanPaths = paths
		}
	}
	if v, ok := settings["exclude_paths"]; ok && v != "" {
		var paths []string
		if err := json.Unmarshal([]byte(v), &paths); err == nil {
			cfg.ExcludePaths = paths
		}
	}
	if v, ok := settings["schedule"]; ok && v != "" {
		cfg.Schedule = v
	}
	if v, ok := settings["scan_paused"]; ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ScanPaused = b
		}
	}
	if v, ok := settings["trash_retention_days"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.TrashRetentionDays = n
		}
	}
	if v, ok := settings["walkers"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ScanWorkers.Walkers = n
		}
	}
	if v, ok := settings["cache_checkers"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ScanWorkers.CacheCheckers = n
		}
	}
	if v, ok := settings["partial_hashers"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ScanWorkers.PartialHashers = n
		}
	}
	if v, ok := settings["full_hashers"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.ScanWorkers.FullHashers = n
		}
	}
}
