package config

import (
	"fmt"
	"strings"
	"time"

	"ai-adv-agent/internal/policy"
)

// PipelineConfig configures one subtask's generate → test → security-review
// pipeline (usecase/devpipeline). Every limit lives here; nothing is
// hard-coded in the pipeline. CommandTimeout == 0 means "the sandbox
// timeout" — that is the one place where zero does not disable a cap.
type PipelineConfig struct {
	// MaxSecurityIterations caps security reviews per subtask (>= 1).
	MaxSecurityIterations int `yaml:"max_security_iterations"`
	// JSONRetries is how many times a reviewer answer that breaks the
	// security_findings.v1 contract is re-requested (>= 0). After that the
	// review fails closed.
	JSONRetries int `yaml:"json_retries"`
	// GeneratorProfile is the default generator profile (a subtask's
	// explicit profile overrides it).
	GeneratorProfile string `yaml:"generator_profile"`
	// Reviewers maps a stack ("go", "vue") to its security reviewer profile.
	Reviewers map[string]string `yaml:"reviewers"`
	// TestCommands maps a stack to the commands that must all exit 0 before
	// a review runs. argv[0] must be in sandbox.allowed_binaries and pass
	// the global command deny list.
	TestCommands map[string][][]string `yaml:"test_commands"`
	// SAST maps a stack to its static analyser command (gosec / eslint).
	SAST map[string][]string `yaml:"sast"`
	// CommandTimeout caps each test/SAST command (0 = the sandbox timeout).
	CommandTimeout time.Duration `yaml:"command_timeout"`
	// TestOutputMaxBytes is the tail of a failed command's output the
	// generator sees in a DATA block.
	TestOutputMaxBytes int `yaml:"test_output_max_bytes"`
	// ReviewFileMaxBytes / ReviewTotalMaxBytes bound the source shown to a
	// reviewer per file and in total.
	ReviewFileMaxBytes  int `yaml:"review_file_max_bytes"`
	ReviewTotalMaxBytes int `yaml:"review_total_max_bytes"`
	// MaxToolRounds caps a role's tool-calling rounds (>= 1).
	MaxToolRounds int `yaml:"max_tool_rounds"`
}

// Stacks the pipeline knows.
const (
	PipelineStackGo  = "go"
	PipelineStackVue = "vue"
)

// DefaultPipeline returns production-safe limits. Fast values for tests and
// e2e live only in testdata/config/*.yaml.
func DefaultPipeline() PipelineConfig {
	return PipelineConfig{
		MaxSecurityIterations: 3,
		JSONRetries:           2,
		GeneratorProfile:      "coder",
		Reviewers: map[string]string{
			PipelineStackGo:  "security-go",
			PipelineStackVue: "security-vue",
		},
		TestCommands: map[string][][]string{
			PipelineStackGo:  {{"go", "vet", "./..."}, {"go", "test", "./..."}},
			PipelineStackVue: {{"vue-tsc", "--noEmit"}, {"vitest", "run"}},
		},
		SAST: map[string][]string{
			PipelineStackGo:  {"gosec", "-fmt=json", "-no-fail", "-quiet", "./..."},
			PipelineStackVue: {"eslint", "-c", "/opt/sandbox/eslint.security.mjs", "-f", "json", "."},
		},
		CommandTimeout:      0, // inherits sandbox.timeout
		TestOutputMaxBytes:  4096,
		ReviewFileMaxBytes:  64 << 10,
		ReviewTotalMaxBytes: 300 << 10,
		MaxToolRounds:       20,
	}
}

// Ceilings for the pipeline limits. Nothing above them is a sane pipeline.
const (
	maxSecurityIterationsCeil = 50
	jsonRetriesCeil           = 10
	maxToolRoundsCeil         = 500
	commandTimeoutCeil        = 30 * time.Minute
	testOutputMaxBytesCeil    = 1 << 20
	reviewFileMaxBytesCeil    = 1 << 20
	reviewTotalMaxBytesCeil   = 8 << 20
)

// validatePipeline checks every pipeline key and every configured command
// against the sandbox allow list and the global command deny list. A
// pipeline that cannot run its checks is refused at load time.
func validatePipeline(p PipelineConfig, sb SandboxConfig) error {
	if p.MaxSecurityIterations < 1 || p.MaxSecurityIterations > maxSecurityIterationsCeil {
		return fmt.Errorf("pipeline.max_security_iterations должен быть в [1, %d], получено %d", maxSecurityIterationsCeil, p.MaxSecurityIterations)
	}
	if p.JSONRetries < 0 || p.JSONRetries > jsonRetriesCeil {
		return fmt.Errorf("pipeline.json_retries должен быть в [0, %d], получено %d", jsonRetriesCeil, p.JSONRetries)
	}
	if strings.TrimSpace(p.GeneratorProfile) == "" {
		return fmt.Errorf("pipeline.generator_profile не задан")
	}
	if p.CommandTimeout < 0 || p.CommandTimeout > commandTimeoutCeil {
		return fmt.Errorf("pipeline.command_timeout должен быть в [0, %s], получено %s", commandTimeoutCeil, p.CommandTimeout)
	}
	if p.TestOutputMaxBytes < 1 || p.TestOutputMaxBytes > testOutputMaxBytesCeil {
		return fmt.Errorf("pipeline.test_output_max_bytes должен быть в [1, %d], получено %d", testOutputMaxBytesCeil, p.TestOutputMaxBytes)
	}
	if p.ReviewFileMaxBytes < 1 || p.ReviewFileMaxBytes > reviewFileMaxBytesCeil {
		return fmt.Errorf("pipeline.review_file_max_bytes должен быть в [1, %d], получено %d", reviewFileMaxBytesCeil, p.ReviewFileMaxBytes)
	}
	if p.ReviewTotalMaxBytes < 1 || p.ReviewTotalMaxBytes > reviewTotalMaxBytesCeil {
		return fmt.Errorf("pipeline.review_total_max_bytes должен быть в [1, %d], получено %d", reviewTotalMaxBytesCeil, p.ReviewTotalMaxBytes)
	}
	if p.ReviewTotalMaxBytes < p.ReviewFileMaxBytes {
		return fmt.Errorf("pipeline.review_total_max_bytes (%d) меньше review_file_max_bytes (%d)", p.ReviewTotalMaxBytes, p.ReviewFileMaxBytes)
	}
	if p.MaxToolRounds < 1 || p.MaxToolRounds > maxToolRoundsCeil {
		return fmt.Errorf("pipeline.max_tool_rounds должен быть в [1, %d], получено %d", maxToolRoundsCeil, p.MaxToolRounds)
	}
	for _, st := range []string{PipelineStackGo, PipelineStackVue} {
		if strings.TrimSpace(p.Reviewers[st]) == "" {
			return fmt.Errorf("pipeline.reviewers.%s не задан", st)
		}
		if len(p.TestCommands[st]) == 0 {
			return fmt.Errorf("pipeline.test_commands.%s пуст — без тестов код не проверяется", st)
		}
		for i, argv := range p.TestCommands[st] {
			if err := checkPipelineCommand(argv, sb, fmt.Sprintf("pipeline.test_commands.%s[%d]", st, i)); err != nil {
				return err
			}
		}
	}
	for st := range p.Reviewers {
		if st != PipelineStackGo && st != PipelineStackVue {
			return fmt.Errorf("pipeline.reviewers: неизвестный стек %q (допустимо: %s, %s)", st, PipelineStackGo, PipelineStackVue)
		}
	}
	for st, argv := range p.SAST {
		if st != PipelineStackGo && st != PipelineStackVue {
			return fmt.Errorf("pipeline.sast: неизвестный стек %q (допустимо: %s, %s)", st, PipelineStackGo, PipelineStackVue)
		}
		if err := checkPipelineCommand(argv, sb, fmt.Sprintf("pipeline.sast.%s", st)); err != nil {
			return err
		}
		switch argv[0] {
		case "gosec", "eslint":
		default:
			return fmt.Errorf("pipeline.sast.%s: неизвестный анализатор %q (gosec или eslint)", st, argv[0])
		}
	}
	return nil
}

// checkPipelineCommand validates one configured pipeline command: a bare
// program name, present in the sandbox allow list, not globally denied.
func checkPipelineCommand(argv []string, sb SandboxConfig, what string) error {
	if len(argv) == 0 {
		return fmt.Errorf("%s: пустая команда", what)
	}
	if strings.ContainsAny(argv[0], `/\`) || strings.HasPrefix(argv[0], "-") {
		return fmt.Errorf("%s: программа %q должна быть именем из списка разрешённых, не путём", what, argv[0])
	}
	allowed := false
	for _, b := range sb.AllowedBinaries {
		if b == argv[0] {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%s: %q нет в sandbox.allowed_binaries (%v)", what, argv[0], sb.AllowedBinaries)
	}
	if !policy.IsSandboxBinary(argv[0]) {
		return fmt.Errorf("%s: %q нет в образе песочницы (допустимо: %v)", what, argv[0], policy.SandboxBinaries)
	}
	if err := policy.CheckGlobalExec(argv); err != nil {
		return fmt.Errorf("%s: %v", what, err)
	}
	return nil
}
