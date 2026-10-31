package googlehealth

import (
	"encoding/json"
	"testing"
)

func TestTotalSteps(t *testing.T) {
	total, err := totalSteps([]json.RawMessage{json.RawMessage(`{"steps":{"countSum":"9000"}}`)})
	if err != nil {
		t.Fatalf("totalSteps() error = %v", err)
	}
	if total != 9000 {
		t.Fatalf("totalSteps() = %d, want 9000", total)
	}
}
