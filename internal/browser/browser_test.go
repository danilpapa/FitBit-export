package browser

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type blockingReader struct {
	chunks []string
}

func (r *blockingReader) Read(buffer []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, errors.New("read after the page ended")
	}
	count := copy(buffer, r.chunks[0])
	r.chunks = r.chunks[1:]
	return count, nil
}

func TestReadPageStopsAtEndOfPage(t *testing.T) {
	page, err := readPage(&blockingReader{chunks: []string{"<html><body>", "text</body>", "</html>\n"}})
	if err != nil {
		t.Fatalf("readPage() error = %v", err)
	}
	if !isComplete(page) {
		t.Fatalf("page = %q", page)
	}
}

func TestReadPageReturnsPartialPage(t *testing.T) {
	page, err := readPage(strings.NewReader("<html><body>"))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("readPage() error = %v, want EOF", err)
	}
	if isComplete(page) {
		t.Fatalf("page = %q, want it to be incomplete", page)
	}
}
