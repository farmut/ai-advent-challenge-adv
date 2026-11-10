package config

import (
	"strings"
	"testing"
	"time"
)

// validatePipeline is the load-time gate of every pipeline limit
// (plan section 7.3): zero/negative where zero is not "off", values above
// the ceilings, and every configured command against the sandbox allow list
// and the global command deny list.

func validPipeline(t *testing.T) (PipelineConfig, SandboxConfig) {
	t.Helper()
	return DefaultPipeline(), DefaultSandbox()
}

func TestPipelineDefaultValid(t *testing.T) {
	p, sb := validPipeline(t)
	if err := validatePipeline(p, sb); err != nil {
		t.Fatalf("DefaultPipeline не проходит валидацию: %v", err)
	}
}

func TestPipelineLimitsValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PipelineConfig)
		want   string
	}{
		{"zero iterations", func(p *PipelineConfig) { p.MaxSecurityIterations = 0 }, "max_security_iterations"},
		{"negative iterations", func(p *PipelineConfig) { p.MaxSecurityIterations = -1 }, "max_security_iterations"},
		{"iterations above ceiling", func(p *PipelineConfig) { p.MaxSecurityIterations = 51 }, "max_security_iterations"},
		{"negative json_retries", func(p *PipelineConfig) { p.JSONRetries = -1 }, "json_retries"},
		{"json_retries above ceiling", func(p *PipelineConfig) { p.JSONRetries = 11 }, "json_retries"},
		{"empty generator", func(p *PipelineConfig) { p.GeneratorProfile = " " }, "generator_profile"},
		{"negative command_timeout", func(p *PipelineConfig) { p.CommandTimeout = -time.Second }, "command_timeout"},
		{"command_timeout above ceiling", func(p *PipelineConfig) { p.CommandTimeout = time.Hour }, "command_timeout"},
		{"zero test_output_max_bytes", func(p *PipelineConfig) { p.TestOutputMaxBytes = 0 }, "test_output_max_bytes"},
		{"test_output above ceiling", func(p *PipelineConfig) { p.TestOutputMaxBytes = 1 << 21 }, "test_output_max_bytes"},
		{"zero review_file_max_bytes", func(p *PipelineConfig) { p.ReviewFileMaxBytes = 0 }, "review_file_max_bytes"},
		{"review_file above ceiling", func(p *PipelineConfig) { p.ReviewFileMaxBytes = 1 << 21 }, "review_file_max_bytes"},
		{"total below file", func(p *PipelineConfig) { p.ReviewTotalMaxBytes = p.ReviewFileMaxBytes - 1 }, "review_total_max_bytes"},
		{"zero max_tool_rounds", func(p *PipelineConfig) { p.MaxToolRounds = 0 }, "max_tool_rounds"},
		{"max_tool_rounds above ceiling", func(p *PipelineConfig) { p.MaxToolRounds = 501 }, "max_tool_rounds"},
		{"missing go reviewer", func(p *PipelineConfig) { delete(p.Reviewers, PipelineStackGo) }, "reviewers.go"},
		{"unknown reviewer stack", func(p *PipelineConfig) { p.Reviewers["ruby"] = "security-ruby" }, "ruby"},
		{"empty go test_commands", func(p *PipelineConfig) { p.TestCommands[PipelineStackGo] = nil }, "test_commands.go"},
		{"empty argv", func(p *PipelineConfig) { p.TestCommands[PipelineStackGo][0] = nil }, "test_commands.go[0]"},
		{"command not in allowed_binaries", func(p *PipelineConfig) {
			p.TestCommands[PipelineStackGo] = [][]string{{"curl", "example.com"}}
		}, "allowed_binaries"},
		{"globally denied command", func(p *PipelineConfig) {
			p.TestCommands[PipelineStackGo] = [][]string{{"go", "get", "./..."}}
		}, "go get"},
		{"unknown sast stack", func(p *PipelineConfig) { p.SAST["ruby"] = []string{"gosec"} }, "sast"},
		{"unknown sast analyzer", func(p *PipelineConfig) { p.SAST[PipelineStackGo] = []string{"go", "version"} }, "неизвестный анализатор"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, sb := validPipeline(t)
			tc.mutate(&p)
			err := validatePipeline(p, sb)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", tc.want, err)
			}
		})
	}
}

// The pipeline section must round-trip through YAML on top of the defaults.
func TestPipelineYAMLLoad(t *testing.T) {
	cfg, err := Load("../../testdata/config/agent.e2e.yaml")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Pipeline.MaxSecurityIterations != 2 || cfg.Pipeline.JSONRetries != 1 {
		t.Fatalf("pipeline не прочитан из yaml: %+v", cfg.Pipeline)
	}
	if cfg.Pipeline.MaxToolRounds != 5 {
		t.Fatalf("max_tool_rounds: %d", cfg.Pipeline.MaxToolRounds)
	}
	if cfg.Pipeline.Reviewers[PipelineStackGo] != "security-go" {
		t.Fatalf("reviewers.go: %q", cfg.Pipeline.Reviewers[PipelineStackGo])
	}
	// The e2e commands must pass the load-time command checks against the
	// default sandbox allow list.
	if err := validatePipeline(cfg.Pipeline, cfg.Sandbox); err != nil {
		t.Fatalf("e2e pipeline не проходит валидацию: %v", err)
	}
}
