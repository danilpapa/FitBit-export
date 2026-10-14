package report

import (
	"encoding/json"
	"strconv"
	"time"
)

type StepRecord struct {
	Steps struct {
		Interval struct {
			StartTime time.Time `json:"startTime"`
			EndTime   time.Time `json:"endTime"`
		} `json:"interval"`

		Count    string `json:"count"`
		CountSum string `json:"countSum"`
	} `json:"steps"`
}

func Total(records []json.RawMessage) (int64, error) {
	var total int64

	for _, raw := range records {
		var record StepRecord

		if err := json.Unmarshal(raw, &record); err != nil {
			return 0, err
		}

		countValue := record.Steps.CountSum
		if countValue == "" {
			countValue = record.Steps.Count
		}

		count, err := strconv.ParseInt(countValue, 10, 64)
		if err != nil {
			return 0, err
		}

		total += count
	}

	return total, nil
}
