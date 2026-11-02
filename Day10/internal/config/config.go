package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Model struct {
	Name    string `yaml:"name"`
	Path    string `yaml:"path"`
	Default bool   `yaml:"default"`
}

type Server struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Bind string `yaml:"bind"`
}

type Inference struct {
	MaxTokens   int     `yaml:"max_tokens"`
	Temperature float64 `yaml:"temperature"`
	TopP        float64 `yaml:"top_p"`
}

type Log struct {
	Level string `yaml:"level"`
	Path  string `yaml:"path"`
}

type Config struct {
	Models      map[string]Model `yaml:"models"`
	Server      Server           `yaml:"server"`
	Inference   Inference        `yaml:"inference"`
	Log         Log              `yaml:"log"`
	_configPath string           `yaml:"-"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	cfg := &Config{_configPath: path}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Server.Bind == "" {
		cfg.Server.Bind = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	}
	if cfg.Server.Host == "" {
		cfg.Server.Host = "127.0.0.1"
	}
	if cfg.Server.Port == 0 {
		cfg.Server.Port = 8080
	}
	if cfg.Inference.MaxTokens == 0 {
		cfg.Inference.MaxTokens = 512
	}
	if cfg.Inference.Temperature == 0 {
		cfg.Inference.Temperature = 0.7
	}
	return cfg, nil
}

func (c *Config) Path() string {
	return c._configPath
}

func (c *Config) SetPath(path string) {
	c._configPath = path
}

func (c *Config) DefaultModel() *Model {
	for _, m := range c.Models {
		if m.Default {
			return &m
		}
	}
	for _, m := range c.Models {
		return &m
	}
	return nil
}

func (c *Config) Save() error {
	if c._configPath == "" {
		return fmt.Errorf("config path not set")
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(c._configPath, data, 0644); err != nil {
		return fmt.Errorf("write config %s: %w", c._configPath, err)
	}
	return nil
}

func (c *Config) SwitchDefault(name string) error {
	found := false
	for key, m := range c.Models {
		m.Default = m.Name == name
		if m.Default {
			found = true
		}
		c.Models[key] = m
	}
	if !found {
		return fmt.Errorf("unknown model %q", name)
	}
	return nil
}

func (c *Config) Clone() *Config {
	clone := &Config{
		Models:    c.Models,
		Server:    c.Server,
		Inference: c.Inference,
		Log:       c.Log,
	}
	clone._configPath = c._configPath
	return clone
}
