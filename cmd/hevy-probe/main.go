// hevy-probe is a throwaway experiment: render a public Hevy workout page in headless
// Chrome and print all of its visible text. It checks whether this works in a cloud run.
//
//	go run ./cmd/hevy-probe [url]
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	defaultURL  = "https://hevy.com/workout/48nSfXwj9Tv"
	pageTimeout = 60 * time.Second
	settleTime  = 2 * time.Second
	pollEvery   = 500 * time.Millisecond
)

var trackerHosts = []string{
	"*.google-analytics.com",
	"*.googletagmanager.com",
	"*.doubleclick.net",
	"*.sentry.io",
	"*.facebook.net",
	"*.hotjar.com",
	"*.amplitude.com",
	"*.vercel-insights.com",
	"*.firstpromoter.com",
	"*.paddle.com",
	"accounts.google.com",
	"www.google.com",
	"www.gstatic.com",
	"appleid.cdn-apple.com",
}

var chromeCandidates = []string{
	"chrome-headless-shell",
	"google-chrome",
	"chromium",
	"chromium-browser",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

func main() {
	target := defaultURL
	if len(os.Args) > 1 {
		target = os.Args[1]
	}

	text, err := run(target)
	if err != nil {
		log.Printf("FAILED: %v", err)
	}
	fmt.Println(text)
	if err != nil {
		os.Exit(1)
	}
}

func run(target string) (string, error) {
	started := time.Now()

	chrome, err := findChrome()
	if err != nil {
		return "", err
	}
	log.Printf("chrome: %s", chrome)

	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "en-US"),
	)
	options = append(options, proxyOptions()...)

	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancelAllocator()
	ctx, cancelBrowser := chromedp.NewContext(allocator, chromedp.WithLogf(log.Printf))
	defer cancelBrowser()
	ctx, cancelTimeout := context.WithTimeout(ctx, pageTimeout)
	defer cancelTimeout()

	requests, err := watchNetwork(ctx)
	if err != nil {
		return "", fmt.Errorf("start chrome: %w", err)
	}

	if err := blockTrackers(ctx); err != nil {
		return "", fmt.Errorf("block trackers: %w", err)
	}

	if _, err := chromedp.Run(ctx, chromedp.Navigate(target)); err != nil {
		requests.report()
		return "", fmt.Errorf("navigate after %s: %w", since(started), err)
	}
	log.Printf("navigated in %s", since(started))

	text, err := waitForWorkout(ctx)
	requests.report()
	if err != nil {
		return text, fmt.Errorf("wait for workout after %s: %w", since(started), err)
	}
	log.Printf("done in %s, %d characters", since(started), len(text))
	return text, nil
}

// waitForWorkout polls the page until it shows sets, then waits a little for the rest.
func waitForWorkout(ctx context.Context) (string, error) {
	const script = `document.body ? document.body.innerText : ""`

	var text string
	for {
		current, err := chromedp.Run(ctx, chromedp.Evaluate[string](script))
		if err != nil {
			return text, err
		}
		text = current

		lower := strings.ToLower(text)
		if strings.Contains(lower, " kg x") || strings.Contains(lower, " кг x") || strings.Contains(lower, "reps") || strings.Contains(lower, "повтор") {
			time.Sleep(settleTime)
			if current, err := chromedp.Run(ctx, chromedp.Evaluate[string](script)); err == nil {
				text = current
			}
			return text, nil
		}

		select {
		case <-ctx.Done():
			return text, errors.New("no sets on the page before the timeout")
		case <-time.After(pollEvery):
		}
	}
}

// blockTrackers fails analytics requests at once. They are not needed for the text,
// and a blocked domain in the cloud would otherwise delay the page load event.
func blockTrackers(ctx context.Context) error {
	var patterns []*network.BlockPattern
	for _, host := range trackerHosts {
		patterns = append(patterns, &network.BlockPattern{URLPattern: "*://" + host + "/*", Block: true})
	}

	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		return err
	}
	_, err := chromedp.Call(ctx, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: patterns})
	return err
}

// proxyOptions passes the cloud egress proxy to Chrome, which ignores HTTPS_PROXY.
func proxyOptions() []chromedp.ExecAllocatorOption {
	raw := firstEnv("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy")
	if raw == "" {
		log.Print("proxy: none")
		return nil
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		log.Printf("proxy: cannot parse it: %v", err)
		return nil
	}
	log.Printf("proxy: %s://%s, credentials: %v", parsed.Scheme, parsed.Host, parsed.User != nil)

	options := []chromedp.ExecAllocatorOption{
		chromedp.ProxyServer(parsed.Scheme + "://" + parsed.Host),
		// The proxy may re-sign TLS with its own CA, which Chrome does not trust.
		chromedp.Flag("ignore-certificate-errors", true),
	}
	if bypass := firstEnv("NO_PROXY", "no_proxy"); bypass != "" {
		options = append(options, chromedp.Flag("proxy-bypass-list", strings.ReplaceAll(bypass, ",", ";")))
	}
	return options
}

type networkLog struct {
	mu       sync.Mutex
	urls     map[network.RequestID]string
	total    int
	problems []string
}

func watchNetwork(ctx context.Context) (*networkLog, error) {
	requests := &networkLog{urls: map[network.RequestID]string{}}

	err := chromedp.Listen(ctx, func(event any) {
		requests.mu.Lock()
		defer requests.mu.Unlock()

		switch e := event.(type) {
		case *network.EventRequestWillBeSent:
			requests.urls[e.RequestID] = e.Request.URL
			requests.total++
		case *network.EventResponseReceived:
			if e.Response.Status >= 400 {
				requests.problems = append(requests.problems, fmt.Sprintf("HTTP %d %s", int(e.Response.Status), e.Response.URL))
			}
		case *network.EventLoadingFailed:
			requests.problems = append(requests.problems, fmt.Sprintf("%s %s", e.ErrorText, requests.urls[e.RequestID]))
		}
	})
	return requests, err
}

func (n *networkLog) report() {
	n.mu.Lock()
	defer n.mu.Unlock()

	hosts := map[string]int{}
	for _, raw := range n.urls {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			hosts[parsed.Host]++
		}
	}
	for host, count := range hosts {
		log.Printf("network host: %s (%d)", host, count)
	}

	log.Printf("network: %d requests, %d problems", n.total, len(n.problems))
	for _, problem := range n.problems {
		log.Printf("network problem: %s", problem)
	}
}

func findChrome() (string, error) {
	if path := os.Getenv("CHROME_PATH"); path != "" {
		return path, nil
	}
	for _, candidate := range chromeCandidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", errors.New("chrome is not installed: set CHROME_PATH or install chrome-headless-shell")
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}

func since(start time.Time) time.Duration {
	return time.Since(start).Round(100 * time.Millisecond)
}
