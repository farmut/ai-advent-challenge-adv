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

// Prompt configures the system prompt template file.
type Prompt struct {
	File string `yaml:"file"`
}

// PromptFileEnv overrides Prompt.File when set.
const PromptFileEnv = "PROMPT_FILE"

// Guard configures the input/output guard. Enabled is a pointer so that a
// missing key means "on" (secure default) rather than false.
type Guard struct {
	Enabled *bool `yaml:"enabled,omitempty"`
}

// GuardEnv overrides Guard.Enabled when set; allowed values are "on"/"off".
const GuardEnv = "GUARD"

type Config struct {
	Models      map[string]Model `yaml:"models"`
	Server      Server           `yaml:"server"`
	Inference   Inference        `yaml:"inference"`
	Log         Log              `yaml:"log"`
	Prompt      Prompt           `yaml:"prompt"`
	Guard       Guard            `yaml:"guard"`
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
		Prompt:    c.Prompt,
	}
	if c.Guard.Enabled != nil {
		v := *c.Guard.Enabled
		clone.Guard.Enabled = &v
	}
	clone._configPath = c._configPath
	return clone
}

// PromptFile returns the prompt template path. Priority: PROMPT_FILE env,
// then prompt.file from YAML. An empty result is a configuration error:
// there is no built-in prompt.
func (c *Config) PromptFile() string {
	if v := os.Getenv(PromptFileEnv); v != "" {
		return v
	}
	return c.Prompt.File
}

// GuardEnabled reports whether the guard is on. Priority: GUARD env ("on" or
// "off"), then guard.enabled from YAML, then on. Any other env value is an
// error so a typo cannot silently disable the guard.
func (c *Config) GuardEnabled() (bool, error) {
	switch v := os.Getenv(GuardEnv); v {
	case "on":
		return true, nil
	case "off":
		return false, nil
	case "":
	default:
		return false, fmt.Errorf("%s must be \"on\" or \"off\", got %q", GuardEnv, v)
	}
	if c.Guard.Enabled != nil {
		return *c.Guard.Enabled, nil
	}
	return true, nil
}
