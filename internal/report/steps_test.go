package report

import (
	"encoding/json"
	"testing"
)

func TestTotalUsesRollupCount(t *testing.T) {
	records := []json.RawMessage{
		json.RawMessage(`{"steps":{"countSum":"9000"}}`),
	}

	total, err := Total(records)
	if err != nil {
		t.Fatalf("Total() error = %v", err)
	}

	if total != 9000 {
		t.Fatalf("Total() = %d, want 9000", total)
	}
}
