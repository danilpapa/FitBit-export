package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	pageTimeout    = 40 * time.Second
	exitWaitTime   = 2 * time.Second
	readBufferSize = 32 * 1024
	pageEnd        = "</html>"
)

var chromeCandidates = []string{
	"google-chrome",
	"chromium",
	"chromium-browser",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

func Render(ctx context.Context, url string) (string, error) {
	chrome, err := findChrome()
	if err != nil {
		return "", err
	}

	profile, err := os.MkdirTemp("", "fitbit-export-chrome-")
	if err != nil {
		return "", fmt.Errorf("create chrome profile: %w", err)
	}
	defer os.RemoveAll(profile)

	ctx, cancel := context.WithTimeout(ctx, pageTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, chrome,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--no-first-run",
		"--lang=en-US",
		"--accept-lang=en-US",
		"--user-data-dir="+profile,
		"--virtual-time-budget=15000",
		"--dump-dom",
		url,
	)
	command.WaitDelay = exitWaitTime

	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("read chrome output: %w", err)
	}
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start chrome: %w", err)
	}

	page, err := readPage(stdout)
	cancel()
	_ = command.Wait()

	if !isComplete(page) {
		return "", fmt.Errorf("render %s: page is incomplete: %v", url, err)
	}
	return page, nil
}

func readPage(output io.Reader) (string, error) {
	var page strings.Builder
	buffer := make([]byte, readBufferSize)

	for !isComplete(page.String()) {
		count, err := output.Read(buffer)
		page.Write(buffer[:count])
		if err != nil {
			return page.String(), err
		}
	}
	return page.String(), nil
}

func isComplete(page string) bool {
	return strings.HasSuffix(strings.TrimSpace(page), pageEnd)
}

func findChrome() (string, error) {
	for _, candidate := range chromeCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("chrome is not installed")
}
