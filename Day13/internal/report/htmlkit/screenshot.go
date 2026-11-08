package htmlkit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"
)

// ChromeEnv overrides the chrome-headless-shell binary path.
const ChromeEnv = "CHROME_HEADLESS_SHELL"

// ErrNoChrome is returned when no chrome-headless-shell binary is available.
var ErrNoChrome = errors.New("chrome-headless-shell not found")

const screenshotWidth = 1200

// FindChrome locates chrome-headless-shell: CHROME_HEADLESS_SHELL env, then
// the newest Playwright cache install.
func FindChrome() (string, error) {
	if p := os.Getenv(ChromeEnv); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", fmt.Errorf("%w: %s=%s: %v", ErrNoChrome, ChromeEnv, p, err)
		}
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoChrome, err)
	}
	matches, _ := filepath.Glob(filepath.Join(home, "Library", "Caches", "ms-playwright",
		"chromium_headless_shell-*", "chrome-headless-shell-mac-arm64", "chrome-headless-shell"))
	if len(matches) == 0 {
		return "", ErrNoChrome
	}
	sort.Strings(matches)
	return matches[len(matches)-1], nil
}

// Screenshot renders htmlPath into pngPath with a 1200xheight viewport.
// Both paths are made absolute.
func Screenshot(ctx context.Context, chrome, htmlPath, pngPath string, height int) error {
	htmlAbs, err := filepath.Abs(htmlPath)
	if err != nil {
		return err
	}
	pngAbs, err := filepath.Abs(pngPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless",
		"--disable-gpu",
		"--hide-scrollbars",
		fmt.Sprintf("--window-size=%d,%d", screenshotWidth, height),
		"--screenshot="+pngAbs,
		"file://"+htmlAbs,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chrome screenshot %s: %w: %s", filepath.Base(htmlAbs), err, out)
	}
	if _, err := os.Stat(pngAbs); err != nil {
		return fmt.Errorf("screenshot not created: %w", err)
	}
	return nil
}
