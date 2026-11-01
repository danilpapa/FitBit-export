package browser

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type chunkReader struct {
	chunks []string
}

func (r *chunkReader) Read(buffer []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, errors.New("read after the page ended")
	}
	count := copy(buffer, r.chunks[0])
	r.chunks = r.chunks[1:]
	return count, nil
}

func fakeChrome(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "chrome")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write fake chrome: %v", err)
	}
	return path
}

func TestReadPageStopsAtEndOfPage(t *testing.T) {
	page, err := readPage(&chunkReader{chunks: []string{"<html><body>", "text</body>", "</html>\n"}})
	if err != nil {
		t.Fatalf("readPage() error = %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(page), pageEnd) {
		t.Fatalf("page = %q", page)
	}
}

func TestReadPageReturnsPartialPage(t *testing.T) {
	page, err := readPage(strings.NewReader("<html><body>"))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("readPage() error = %v, want EOF", err)
	}
	if page != "<html><body>" {
		t.Fatalf("page = %q", page)
	}
}

func TestRenderAcceptsTextAfterPage(t *testing.T) {
	chrome := fakeChrome(t, `echo '<html><body>sets</body></html>'; echo 'finished'`)

	page, err := render(context.Background(), chrome, "https://hevy.com/workout/example")
	if err != nil {
		t.Fatalf("render() error = %v", err)
	}
	if !strings.Contains(page, "sets") {
		t.Fatalf("page = %q", page)
	}
}

func TestRenderExplainsFailure(t *testing.T) {
	chrome := fakeChrome(t, `echo 'ERROR:dbus/bus.cc' >&2; echo 'ERROR:net: ERR_TUNNEL_CONNECTION_FAILED' >&2; exit 1`)

	_, err := render(context.Background(), chrome, "https://hevy.com/workout/example")
	if err == nil {
		t.Fatal("render() error = nil")
	}
	if !strings.Contains(err.Error(), "ERR_TUNNEL_CONNECTION_FAILED") {
		t.Fatalf("error = %v, want the reason from chrome", err)
	}
	if strings.Contains(err.Error(), "dbus") {
		t.Fatalf("error = %v, want no dbus noise", err)
	}
}

func TestRenderWithRetryRecoversFromFailure(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	chrome := fakeChrome(t, `if [ -f '`+marker+`' ]; then echo '<html></html>'; else touch '`+marker+`'; exit 1; fi`)

	page, err := renderWithRetry(context.Background(), chrome, "https://hevy.com/workout/example")
	if err != nil {
		t.Fatalf("renderWithRetry() error = %v", err)
	}
	if !strings.Contains(page, pageEnd) {
		t.Fatalf("page = %q", page)
	}
}
