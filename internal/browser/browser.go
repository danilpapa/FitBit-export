package browser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	pageTimeout     = 90 * time.Second
	exitWaitTime    = 2 * time.Second
	readBufferSize  = 32 * 1024
	renderAttempts  = 2
	pageEnd         = "</html>"
	chromeNoise     = "dbus"
	maxChromeDetail = 3
	disabledDBus    = "DBUS_SESSION_BUS_ADDRESS=disabled:"
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
	return renderWithRetry(ctx, chrome, url)
}

func renderWithRetry(ctx context.Context, chrome string, url string) (string, error) {
	var page string
	var err error

	for attempt := 1; attempt <= renderAttempts; attempt++ {
		page, err = render(ctx, chrome, url)
		if err == nil || ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return page, err
		}
		log.Printf("chrome: attempt %d failed: %v", attempt, err)
	}
	return "", err
}

func render(ctx context.Context, chrome string, url string) (string, error) {
	profile, err := os.MkdirTemp("", "fitbit-export-chrome-")
	if err != nil {
		return "", fmt.Errorf("create chrome profile: %w", err)
	}
	defer os.RemoveAll(profile)

	started := time.Now()
	ctx, cancel := context.WithTimeout(ctx, pageTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, chrome,
		"--headless=new",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--no-first-run",
		"--lang=en-US",
		"--accept-lang=en-US",
		"--user-data-dir="+profile,
		"--virtual-time-budget=15000",
		"--dump-dom",
		url,
	)
	command.Env = append(os.Environ(), disabledDBus)
	command.WaitDelay = exitWaitTime

	var stderr bytes.Buffer
	command.Stderr = &stderr

	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("read chrome output: %w", err)
	}
	if err := command.Start(); err != nil {
		return "", fmt.Errorf("start chrome: %w", err)
	}

	page, readErr := readPage(stdout)
	elapsed := time.Since(started).Round(time.Second)
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	cancel()
	_ = command.Wait()

	if strings.Contains(page, pageEnd) {
		log.Printf("chrome: rendered %s in %s", url, elapsed)
		return page, nil
	}

	detail := chromeDetail(stderr.String())
	if timedOut {
		return "", fmt.Errorf("render %s: chrome timed out after %s; chrome: %s: %w", url, elapsed, detail, context.DeadlineExceeded)
	}
	return "", fmt.Errorf("render %s: page is incomplete after %d bytes in %s: %v; chrome: %s", url, len(page), elapsed, readErr, detail)
}

func readPage(output io.Reader) (string, error) {
	var page strings.Builder
	buffer := make([]byte, readBufferSize)

	for !strings.HasSuffix(strings.TrimSpace(page.String()), pageEnd) {
		count, err := output.Read(buffer)
		page.Write(buffer[:count])
		if err != nil {
			return page.String(), err
		}
	}
	return page.String(), nil
}

func chromeDetail(stderr string) string {
	var lines []string
	for line := range strings.Lines(stderr) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.Contains(line, chromeNoise) {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "no output"
	}
	return strings.Join(lines[max(0, len(lines)-maxChromeDetail):], " | ")
}

func findChrome() (string, error) {
	for _, candidate := range chromeCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("chrome is not installed")
}
