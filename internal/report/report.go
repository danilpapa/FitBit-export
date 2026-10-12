package report

import (
	"encoding/json"
	"time"
)

type Report struct {
	GeneratedAt time.Time         `json:"generatedAt"`
	Period      Period            `json:"period"`
	Steps       []json.RawMessage `json:"steps"`
}

type Period struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}