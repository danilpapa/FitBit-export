// Package hevy reads public Hevy workout pages with headless Chrome.
package hevy

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	pageTimeout  = 45 * time.Second
	trustTimeout = 15 * time.Second
	settleTime   = time.Second
	pollEvery    = 500 * time.Millisecond

	locale         = "ru_RU"
	acceptLanguage = "ru-RU,ru;q=0.9"
	textScript     = `document.body ? document.body.innerText : ""`

	// Hevy picks the language from navigator.language, and Chrome on macOS ignores
	// --lang, so the page gets the language before its own scripts run.
	languageScript = `Object.defineProperty(navigator, "language", {get: () => "ru-RU"});
Object.defineProperty(navigator, "languages", {get: () => ["ru-RU", "ru"]});`
)

var chromeCandidates = []string{
	"chrome-headless-shell",
	"google-chrome",
	"chromium",
	"chromium-browser",
	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
}

// trackerHosts are blocked: the text does not need them, and in a cloud run
// a closed domain would hold up the page load event.
var trackerHosts = []string{
	"*.google-analytics.com",
	"*.googletagmanager.com",
	"*.doubleclick.net",
	"*.sentry.io",
	"*.facebook.net",
	"*.amplitude.com",
	"*.vercel-insights.com",
	"*.firstpromoter.com",
	"*.paddle.com",
	"accounts.google.com",
	"www.google.com",
	"www.gstatic.com",
	"appleid.cdn-apple.com",
}

// Pages opens one Chrome on first use and renders each page in its own tab.
type Pages struct {
	timezone string

	once    sync.Once
	browser context.Context
	cancel  context.CancelFunc
	err     error
}

// New renders pages in the time zone of location, so dates match the device clock.
func New(location *time.Location) *Pages {
	return &Pages{timezone: timezoneID(time.Now().In(location))}
}

// Workout returns the sets of a public workout page in the format of workout notes.
func (p *Pages) Workout(ctx context.Context, link string) (string, error) {
	if FindLink(link) != link {
		return "", fmt.Errorf("not a Hevy workout link: %s", link)
	}

	p.once.Do(p.start)
	if p.err != nil {
		return "", p.err
	}

	tab, cancelTab := chromedp.NewContext(p.browser)
	defer cancelTab()
	stop := context.AfterFunc(ctx, cancelTab)
	defer stop()
	tab, cancelTimeout := context.WithTimeout(tab, pageTimeout)
	defer cancelTimeout()

	started := time.Now()
	if err := p.prepare(tab); err != nil {
		return "", fmt.Errorf("prepare Chrome tab: %w", err)
	}
	if _, err := chromedp.Run(tab, chromedp.Navigate(link)); err != nil {
		return "", fmt.Errorf("open %s: %w", link, err)
	}
	page, err := waitForSets(tab)
	if err != nil {
		return "", fmt.Errorf("read %s after %s: %w", link, time.Since(started).Round(time.Second), err)
	}

	log.Printf("hevy: read %s in %s", link, time.Since(started).Round(100*time.Millisecond))
	return Clean(page), nil
}

func (p *Pages) Close() {
	if p.cancel != nil {
		p.cancel()
	}
}

func (p *Pages) start() {
	chrome, err := findChrome()
	if err != nil {
		p.err = err
		return
	}

	options := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chrome),
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "ru-RU"),
	)
	options = append(options, proxyOptions()...)

	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(), options...)
	browser, cancelBrowser := chromedp.NewContext(allocator)
	p.browser = browser
	p.cancel = func() {
		cancelBrowser()
		cancelAllocator()
	}

	// Start the browser on a context without a timeout, so that it outlives each tab.
	if err := chromedp.Do(browser); err != nil {
		p.err = fmt.Errorf("start chrome %s: %w", chrome, err)
		return
	}
	log.Printf("hevy: chrome %s, time zone %s", chrome, p.timezone)
}

func (p *Pages) prepare(tab context.Context) error {
	var patterns []*network.BlockPattern
	for _, host := range trackerHosts {
		patterns = append(patterns, &network.BlockPattern{URLPattern: "*://" + host + "/*", Block: true})
	}

	if _, err := chromedp.Call(tab, network.Enable, network.EnableParams{}); err != nil {
		return err
	}
	if _, err := chromedp.Call(tab, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: patterns}); err != nil {
		return err
	}
	if _, err := chromedp.Call(tab, network.SetExtraHTTPHeaders, network.SetExtraHTTPHeadersParams{
		Headers: network.Headers{"Accept-Language": acceptLanguage},
	}); err != nil {
		return err
	}
	if _, err := chromedp.Call(tab, emulation.SetLocaleOverride, emulation.SetLocaleOverrideParams{Locale: locale}); err != nil {
		return err
	}
	if _, err := chromedp.Call(tab, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: languageScript,
	}); err != nil {
		return err
	}
	_, err := chromedp.Call(tab, emulation.SetTimezoneOverride, emulation.SetTimezoneOverrideParams{TimezoneID: p.timezone})
	return err
}

// waitForSets polls the page until it shows sets, then gives it a moment to finish.
func waitForSets(tab context.Context) (string, error) {
	var text string
	for {
		current, err := chromedp.Run(tab, chromedp.Evaluate[string](textScript))
		if err != nil {
			return text, err
		}
		text = current

		if strings.Contains(text, " кг x") || strings.Contains(text, "повтор") {
			time.Sleep(settleTime)
			if current, err := chromedp.Run(tab, chromedp.Evaluate[string](textScript)); err == nil {
				text = current
			}
			return text, nil
		}

		select {
		case <-tab.Done():
			return text, errors.New("the page shows no sets")
		case <-time.After(pollEvery):
		}
	}
}

// timezoneID names the offset of moment, for example Etc/GMT-3 for UTC+3, because
// the device clock has an offset and no zone name. The sign of Etc/GMT zones is
// inverted by convention. Offsets that are not whole hours fall back to UTC.
func timezoneID(moment time.Time) string {
	_, offset := moment.Zone()
	if offset == 0 || offset%3600 != 0 {
		return "UTC"
	}
	return fmt.Sprintf("Etc/GMT%+d", -offset/3600)
}

// proxyOptions sends Chrome through the egress proxy of a cloud run. Chrome
// ignores HTTPS_PROXY, and it does not use the system certificates on Linux.
func proxyOptions() []chromedp.ExecAllocatorOption {
	raw := firstEnv("HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy")
	if raw == "" {
		return nil
	}
	proxy, err := url.Parse(raw)
	if err != nil || proxy.Host == "" {
		log.Printf("hevy: cannot use proxy: %v", err)
		return nil
	}

	options := []chromedp.ExecAllocatorOption{chromedp.ProxyServer(proxy.Scheme + "://" + proxy.Host)}
	if bypass := firstEnv("NO_PROXY", "no_proxy"); bypass != "" {
		options = append(options, chromedp.Flag("proxy-bypass-list", strings.ReplaceAll(bypass, ",", ";")))
	}

	keys, err := trustedKeys()
	if err != nil {
		log.Printf("hevy: Chrome will check certificates on its own: %v", err)
		return options
	}
	log.Printf("hevy: proxy %s, trusting %d keys verified by the system", proxy.Host, strings.Count(keys, ",")+1)
	return append(options, chromedp.Flag("ignore-certificate-errors-spki-list", keys))
}

// trustedKeys opens hevy.com through the proxy with the system certificates and
// returns the keys of the chain that the system verified. Chrome then trusts that
// chain and nothing else, instead of ignoring every certificate error.
func trustedKeys() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), trustTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://hevy.com/", nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	response.Body.Close()

	if response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		return "", errors.New("no verified certificate chain")
	}
	var keys []string
	for _, cert := range response.TLS.VerifiedChains[0] {
		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		keys = append(keys, base64.StdEncoding.EncodeToString(sum[:]))
	}
	return strings.Join(keys, ","), nil
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
	return "", errors.New("chrome is not installed: set CHROME_PATH")
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := os.Getenv(key); value != "" {
			return value
		}
	}
	return ""
}
