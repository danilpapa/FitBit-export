package googlehealth

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDailyRollupRequestUsesCivilDates(t *testing.T) {
	start := time.Date(2026, time.October, 2, 15, 30, 0, 0, time.UTC)
	end := time.Date(2026, time.October, 31, 8, 0, 0, 0, time.UTC)

	body, err := json.Marshal(newDailyRollupRequest(start, end))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	want := `{"range":{"start":{"date":{"year":2026,"month":10,"day":2}},` +
		`"end":{"date":{"year":2026,"month":10,"day":31}}},"windowSizeDays":1,"pageSize":90}`
	if string(body) != want {
		t.Fatalf("request = %s, want %s", body, want)
	}
}
