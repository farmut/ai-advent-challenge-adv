package mlx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	serverBinName = "mlx_lm.server"
	// localServerBin is the venv created by "make setup", relative to the
	// module root.
	localServerBin = ".venv/bin/mlx_lm.server"
	// sharedServerBin is the shared Day6 venv relative to the module root
	// (last-resort fallback for the original stand).
	sharedServerBin = "../Day6/.venv/bin/mlx_lm.server"
	healthPath      = "/health"
	logFileName     = "mlx-server.log"

	// ServerBinEnv overrides the mlx_lm.server binary path.
	ServerBinEnv = "MLX_SERVER_BIN"
)

// ServerBin returns the mlx_lm.server binary. Order: MLX_SERVER_BIN env,
// <module root>/.venv/bin/mlx_lm.server when it exists ("make setup"),
// mlx_lm.server in PATH, then ../Day6/.venv/bin/mlx_lm.server relative to
// the module root (the nearest parent of the working directory with go.mod).
func ServerBin() string {
	return serverBin(os.Getenv, exec.LookPath, os.Getwd)
}

func serverBin(getenv func(string) string, lookPath func(string) (string, error), getwd func() (string, error)) string {
	if v := getenv(ServerBinEnv); v != "" {
		return v
	}
	root := ""
	if wd, err := getwd(); err == nil {
		root = moduleRoot(wd)
	}
	if root != "" {
		local := filepath.Join(root, localServerBin)
		if fi, err := os.Stat(local); err == nil && !fi.IsDir() {
			return local
		}
	}
	if p, err := lookPath(serverBinName); err == nil {
		return p
	}
	if root != "" {
		return filepath.Clean(filepath.Join(root, sharedServerBin))
	}
	return serverBinName
}

// moduleRoot returns the nearest directory at or above dir with go.mod, or "".
func moduleRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return ""
		}
		d = p
	}
}

type Server struct {
	bind    string
	logDir  string
	cmd     *exec.Cmd
	mu      sync.Mutex
	started time.Time
	load    time.Duration
	logger  *slog.Logger
	logFile *os.File
}

// NewServer creates a server manager. mlx_lm.server output is written to
// <logDir>/mlx-server.log.
func NewServer(bind, logDir string, logger *slog.Logger) *Server {
	return &Server{bind: bind, logDir: logDir, logger: logger}
}

func (s *Server) Start(modelPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		if err := s.stopLocked(); err != nil {
			s.logger.Warn("failed to stop previous server", slog.String("error", err.Error()))
		}
	}

	modelPath, err := filepath.Abs(modelPath)
	if err != nil {
		return fmt.Errorf("resolve model path: %w", err)
	}

	logDir, err := filepath.Abs(s.logDir)
	if err != nil {
		return fmt.Errorf("resolve log dir: %w", err)
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, logFileName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("open mlx log: %w", err)
	}

	s.cmd = exec.Command(ServerBin(),
		"--model", modelPath,
		"--host", hostFromBind(s.bind),
		"--port", portFromBind(s.bind),
	)
	s.cmd.Stdout = logFile
	s.cmd.Stderr = logFile
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	s.logFile = logFile
	s.started = time.Now()

	if err := s.cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start mlx_lm.server: %w", err)
	}

	s.logger.Info("mlx_lm.server started",
		slog.String("model", modelPath),
		slog.String("bind", s.bind),
		slog.Int("pid", s.cmd.Process.Pid),
	)

	if err := s.waitReady(context.Background()); err != nil {
		s.stopLocked()
		return fmt.Errorf("server not ready: %w", err)
	}
	s.load = time.Since(s.started)
	s.logger.Info("mlx_lm.server ready", slog.Duration("load_time", s.load))
	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *Server) stopLocked() error {
	if s.cmd == nil || s.cmd.Process == nil {
		return nil
	}
	pid := s.cmd.Process.Pid
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		s.logger.Warn("failed to kill process group", slog.String("error", err.Error()))
		_ = s.cmd.Process.Kill()
	}
	_, _ = s.cmd.Process.Wait()
	s.cmd = nil
	if s.logFile != nil {
		_ = s.logFile.Close()
		s.logFile = nil
	}
	s.logger.Info("mlx_lm.server stopped", slog.Int("pid", pid))
	return nil
}

func (s *Server) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+s.bind+healthPath, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("health status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func (s *Server) waitReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.HealthCheck(ctx); err == nil {
				return nil
			}
		}
	}
}

func (s *Server) LoadTime() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.load
}

func hostFromBind(bind string) string {
	for i := len(bind) - 1; i >= 0; i-- {
		if bind[i] == ':' {
			return bind[:i]
		}
	}
	return "127.0.0.1"
}

func portFromBind(bind string) string {
	for i := len(bind) - 1; i >= 0; i-- {
		if bind[i] == ':' {
			return bind[i+1:]
		}
	}
	return "8080"
}
