package exporter

import (
	"context"
	"fmt"
	"time"

	"fitbit-export/internal/healthapi"
	"fitbit-export/internal/report"
)

type Exporter struct {
	api *healthapi.Client
}

func New(api *healthapi.Client) *Exporter {
	return &Exporter{api: api}
}

func (e *Exporter) Run(
	ctx context.Context,
	start time.Time,
	end time.Time,
) (report.Report, error) {
	if !start.Before(end) {
		return report.Report{}, fmt.Errorf("start must be before end")
	}

	steps, err := e.api.ListSteps(ctx, start, end)
	if err != nil {
		return report.Report{}, fmt.Errorf("export steps: %w", err)
	}

	stepsTotal, err := report.Total(steps)
	if err != nil {
		return report.Report{}, fmt.Errorf("calculate total steps: %w", err)
	}

	return report.Report{
		GeneratedAt: time.Now(),
		Period: report.Period{
			Start: start,
			End:   end,
		},
		Steps: steps,
		StepsTotal: stepsTotal,
	}, nil
}