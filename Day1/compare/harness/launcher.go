package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"harness/internal/transcript"
)

const gracefulWait = 5 * time.Second

// botProc — запущенный процесс бота (нативный или в контейнере).
type botProc struct {
	cmd       *exec.Cmd
	container string // для docker: имя контейнера
	logFile   *os.File
	exited    chan struct{}
	waitErr   error
}

// startNative запускает бинарь с заданным окружением; stdout/stderr — в logFile.
func startNative(bin string, env []string, logFile *os.File) (*botProc, error) {
	cmd := exec.Command(bin)
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return start(cmd, "", logFile)
}

// startDocker запускает linux-бинарь в alpine с примонтированным каталогом binDir.
func startDocker(name, binDir, binName string, env []string, logFile *os.File) (*botProc, error) {
	args := []string{"run", "--rm", "--name", name, "-v", binDir + ":/w:ro"}
	for _, kv := range env {
		args = append(args, "-e", kv)
	}
	args = append(args, dockerImage, "/w/"+binName)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return start(cmd, name, logFile)
}

func start(cmd *exec.Cmd, container string, logFile *os.File) (*botProc, error) {
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	p := &botProc{cmd: cmd, container: container, logFile: logFile, exited: make(chan struct{})}
	go func() {
		p.waitErr = cmd.Wait()
		close(p.exited)
	}()
	return p, nil
}

// Exited сообщает, завершился ли процесс сам (например, упал на старте).
func (p *botProc) Exited() bool {
	select {
	case <-p.exited:
		return true
	default:
		return false
	}
}

// Stop шлёт SIGTERM и ждёт gracefulWait; потом SIGKILL.
func (p *botProc) Stop() *transcript.Shutdown {
	res := &transcript.Shutdown{Signal: "SIGTERM", ExitCode: -1}
	if p.Exited() {
		res.Signal = "none (process already exited)"
		res.ExitCode = exitCode(p.waitErr)
		return res
	}
	start := time.Now()
	if p.container != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = exec.CommandContext(ctx, "docker", "kill", "--signal=TERM", p.container).Run()
		cancel()
	} else {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.exited:
		res.Graceful = true
	case <-time.After(gracefulWait):
		res.Killed = true
		if p.container != "" {
			_ = exec.Command("docker", "kill", p.container).Run()
		}
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
	res.ElapsedMs = time.Since(start).Milliseconds()
	res.ExitCode = exitCode(p.waitErr)
	if res.Graceful && res.ExitCode != 0 {
		res.Graceful = false // завершился, но с ошибкой
	}
	return res
}

// Kill — аварийное завершение без метрик.
func (p *botProc) Kill() {
	if p.container != "" {
		_ = exec.Command("docker", "rm", "-f", p.container).Run()
	}
	if !p.Exited() {
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// baseEnv — минимальное окружение без секретов и прокси хоста.
func baseEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "PATH", "HOME", "TMPDIR", "LANG":
			env = append(env, kv)
		}
	}
	return env
}
