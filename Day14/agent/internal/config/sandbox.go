package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ai-adv-agent/internal/policy"
)

// SandboxConfig configures the isolated runner behind run_command and the
// loop's test/SAST commands. There is deliberately no "disabled" switch:
// without an image no command runs at all (fail-closed), and the loop refuses
// to start.
type SandboxConfig struct {
	// Runtime is the only supported isolation backend: "docker".
	Runtime string `yaml:"runtime"`
	// DockerBin is the docker CLI: a bare name looked up in PATH or an
	// absolute path.
	DockerBin string `yaml:"docker_bin"`
	// Image is the sandbox image (make sandbox-image). Pin it by digest
	// (name@sha256:…) for reproducibility. Empty: no sandbox, run_command is
	// refused.
	Image string `yaml:"image"`
	// Platform is the image platform passed as docker run --platform
	// ("linux/amd64"). Empty: the image's own. Must match the built image
	// (make sandbox-image builds SANDBOX_PLATFORM, default linux/amd64).
	Platform string `yaml:"platform"`
	// WorkspaceRoot bounds what may be mounted: a command's workdir must lie
	// inside it. Resolved against the config file's directory; empty falls
	// back to profiles.write_root.
	WorkspaceRoot string `yaml:"workspace_root"`
	// Templates maps a stack ("go", "vue") to the template directory the
	// loop workspace seeds a new run's base from. Resolved against the
	// config file's directory. A stack without a template cannot be seeded.
	Templates map[string]string `yaml:"templates"`
	// Timeout caps one command; a per-call timeout may only be shorter.
	Timeout time.Duration `yaml:"timeout"`
	// KillTimeout bounds the docker kill/ps/version calls the runner makes
	// around a command.
	KillTimeout time.Duration `yaml:"kill_timeout"`
	// MaxOutputBytes caps stdout and stderr each; the rest is dropped and the
	// result is marked truncated.
	MaxOutputBytes int `yaml:"max_output_bytes"`
	// Memory, CPUs, Pids and TmpfsSize are container limits. The /tmp tmpfs
	// (working copy, GOCACHE) is charged to the memory limit too.
	Memory    string `yaml:"memory"`
	CPUs      string `yaml:"cpus"`
	Pids      int    `yaml:"pids"`
	TmpfsSize string `yaml:"tmpfs_size"`
	// CopyMaxFiles / CopyMaxBytes bound the task tree sbx-run copies into
	// the container.
	CopyMaxFiles int   `yaml:"copy_max_files"`
	CopyMaxBytes int64 `yaml:"copy_max_bytes"`
	// AllowedBinaries narrows policy.SandboxBinaries; it cannot widen it.
	AllowedBinaries []string `yaml:"allowed_binaries"`
}

// RuntimeDocker is the only accepted sandbox.runtime.
const RuntimeDocker = "docker"

// DefaultSandbox returns safe production limits; the image is not set.
func DefaultSandbox() SandboxConfig {
	return SandboxConfig{
		Runtime:         RuntimeDocker,
		DockerBin:       "docker",
		Timeout:         5 * time.Minute,
		KillTimeout:     10 * time.Second,
		MaxOutputBytes:  256 << 10,
		Memory:          "1g",
		CPUs:            "2",
		Pids:            256,
		TmpfsSize:       "1g",
		CopyMaxFiles:    5000,
		CopyMaxBytes:    50 << 20,
		AllowedBinaries: append([]string(nil), policy.SandboxBinaries...),
	}
}

// Enabled reports whether a sandbox image is configured.
func (s SandboxConfig) Enabled() bool { return strings.TrimSpace(s.Image) != "" }

// RequireImage is the loop's precondition: no image, no loop.
func (s SandboxConfig) RequireImage() error {
	if !s.Enabled() {
		return fmt.Errorf("sandbox.image не задан: без песочницы запуск кода запрещён (соберите образ: make sandbox-image)")
	}
	return nil
}

var (
	// Docker image reference: no leading '-', so it can never be read as a flag.
	imageRe    = regexp.MustCompile(`^[a-z0-9][a-z0-9._/:@-]*$`)
	platformRe = regexp.MustCompile(`^[a-z0-9]+/[a-z0-9_]+(/[a-z0-9]+)?$`)
	sizeRe     = regexp.MustCompile(`^[1-9][0-9]*[bkmg]?$`)
)

// validateSandbox checks every sandbox key. Zero limits are errors: none of
// them means "unlimited".
func validateSandbox(s SandboxConfig) error {
	if s.Runtime != RuntimeDocker {
		return fmt.Errorf("sandbox.runtime: поддерживается только %q, получено %q", RuntimeDocker, s.Runtime)
	}
	bin := strings.TrimSpace(s.DockerBin)
	if bin == "" || strings.HasPrefix(bin, "-") || strings.ContainsAny(bin, " \t\n") ||
		(strings.ContainsRune(bin, filepath.Separator) && !filepath.IsAbs(bin)) {
		return fmt.Errorf("sandbox.docker_bin: нужно имя программы или абсолютный путь, получено %q", s.DockerBin)
	}
	if s.Image != "" && !imageRe.MatchString(s.Image) {
		return fmt.Errorf("sandbox.image: недопустимая ссылка на образ %q", s.Image)
	}
	if s.Platform != "" && !platformRe.MatchString(s.Platform) {
		return fmt.Errorf("sandbox.platform: ожидается os/arch, например linux/amd64, получено %q", s.Platform)
	}
	if s.Enabled() {
		root := strings.TrimSpace(s.WorkspaceRoot)
		if root == "" {
			return fmt.Errorf("sandbox.workspace_root обязателен при заданном sandbox.image (или profiles.write_root)")
		}
		if filepath.IsAbs(root) && filepath.Clean(root) == string(filepath.Separator) {
			return fmt.Errorf("sandbox.workspace_root не может быть корнем файловой системы")
		}
	}
	if s.Timeout <= 0 {
		return fmt.Errorf("sandbox.timeout должен быть > 0, получено %s", s.Timeout)
	}
	if s.KillTimeout <= 0 {
		return fmt.Errorf("sandbox.kill_timeout должен быть > 0, получено %s", s.KillTimeout)
	}
	if s.MaxOutputBytes <= 0 {
		return fmt.Errorf("sandbox.max_output_bytes должен быть > 0, получено %d", s.MaxOutputBytes)
	}
	for stack, dir := range s.Templates {
		switch stack {
		case PipelineStackGo, PipelineStackVue:
		default:
			return fmt.Errorf("sandbox.templates: неизвестный стек %q (допустимо: %s, %s)", stack, PipelineStackGo, PipelineStackVue)
		}
		if strings.TrimSpace(dir) == "" {
			return fmt.Errorf("sandbox.templates[%s]: путь к шаблону пуст", stack)
		}
	}
	if !sizeRe.MatchString(strings.ToLower(s.Memory)) {
		return fmt.Errorf("sandbox.memory: ожидается размер вида 512m или 1g, получено %q", s.Memory)
	}
	if !sizeRe.MatchString(strings.ToLower(s.TmpfsSize)) {
		return fmt.Errorf("sandbox.tmpfs_size: ожидается размер вида 512m или 1g, получено %q", s.TmpfsSize)
	}
	cpus, err := strconv.ParseFloat(s.CPUs, 64)
	if err != nil || cpus <= 0 || cpus > 256 {
		return fmt.Errorf("sandbox.cpus: ожидается число > 0, получено %q", s.CPUs)
	}
	if s.Pids <= 0 {
		return fmt.Errorf("sandbox.pids должен быть > 0, получено %d", s.Pids)
	}
	if s.CopyMaxFiles <= 0 {
		return fmt.Errorf("sandbox.copy_max_files должен быть > 0, получено %d", s.CopyMaxFiles)
	}
	if s.CopyMaxBytes <= 0 {
		return fmt.Errorf("sandbox.copy_max_bytes должен быть > 0, получено %d", s.CopyMaxBytes)
	}
	if len(s.AllowedBinaries) == 0 {
		return fmt.Errorf("sandbox.allowed_binaries пуст: укажите программы из %v", policy.SandboxBinaries)
	}
	for _, b := range s.AllowedBinaries {
		if !policy.IsSandboxBinary(b) {
			return fmt.Errorf("sandbox.allowed_binaries: %q нет в образе песочницы (допустимо: %v)", b, policy.SandboxBinaries)
		}
	}
	return nil
}
