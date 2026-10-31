package report

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minutesAsleep = "asleep"
	minutesAwake  = "awake"
)

type interval struct {
	StartTime      string `json:"startTime"`
	StartUTCOffset string `json:"startUtcOffset"`
	EndTime        string `json:"endTime"`
	EndUTCOffset   string `json:"endUtcOffset"`
}

func (i interval) start() (time.Time, error) {
	return localTime(i.StartTime, i.StartUTCOffset)
}

func (i interval) end() (time.Time, error) {
	return localTime(i.EndTime, i.EndUTCOffset)
}

type night struct {
	start   time.Time
	end     time.Time
	minutes map[string]float64
}

func collect(points []json.RawMessage, dateGroup string, m metric) (map[string]float64, error) {
	series := map[string]float64{}

	for _, raw := range points {
		var point map[string]any
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode %s point: %w", m.dataType, err)
		}

		date, err := pointDate(point, dateGroup)
		if err != nil {
			return nil, fmt.Errorf("decode %s point: %w", m.dataType, err)
		}

		group, _ := point[m.group].(map[string]any)
		var total float64
		found := false
		for _, field := range m.fields {
			if value, ok := number(group[field]); ok {
				total += value
				found = true
			}
		}
		if !found {
			continue
		}

		if m.scale != 0 {
			total *= m.scale
		}
		series[date] = total
	}

	return series, nil
}

func pointDate(point map[string]any, dateGroup string) (string, error) {
	group, _ := point[dateGroup].(map[string]any)
	date, ok := group["date"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing %s.date", dateGroup)
	}

	year, _ := date["year"].(float64)
	month, _ := date["month"].(float64)
	day, _ := date["day"].(float64)
	return fmt.Sprintf("%04d-%02d-%02d", int(year), int(month), int(day)), nil
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSuffix(typed, "s"), 64)
		return parsed, err == nil
	}
	return 0, false
}

func latestStepStart(points []json.RawMessage) (time.Time, error) {
	var latest time.Time

	for _, raw := range points {
		var point struct {
			Steps struct {
				Interval interval `json:"interval"`
			} `json:"steps"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return time.Time{}, fmt.Errorf("decode steps point: %w", err)
		}

		start, err := point.Steps.Interval.start()
		if err != nil {
			return time.Time{}, fmt.Errorf("decode steps start: %w", err)
		}
		if start.After(latest) {
			latest = start
		}
	}

	return latest, nil
}

func decodeNights(points []json.RawMessage) (map[string]night, error) {
	nights := map[string]night{}

	for _, raw := range points {
		var point struct {
			Sleep struct {
				Interval interval `json:"interval"`
				Metadata struct {
					MainSleep bool `json:"mainSleep"`
				} `json:"metadata"`
				Summary struct {
					MinutesAsleep float64 `json:"minutesAsleep,string"`
					MinutesAwake  float64 `json:"minutesAwake,string"`
					StagesSummary []struct {
						Type    string  `json:"type"`
						Minutes float64 `json:"minutes,string"`
					} `json:"stagesSummary"`
				} `json:"summary"`
			} `json:"sleep"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode sleep point: %w", err)
		}
		if !point.Sleep.Metadata.MainSleep {
			continue
		}

		start, err := point.Sleep.Interval.start()
		if err != nil {
			return nil, fmt.Errorf("decode sleep start: %w", err)
		}
		end, err := point.Sleep.Interval.end()
		if err != nil {
			return nil, fmt.Errorf("decode sleep end: %w", err)
		}

		summary := point.Sleep.Summary
		current := night{
			start: start,
			end:   end,
			minutes: map[string]float64{
				minutesAsleep: summary.MinutesAsleep,
				minutesAwake:  summary.MinutesAwake,
			},
		}
		for _, stage := range summary.StagesSummary {
			current.minutes[stage.Type] = stage.Minutes
		}

		date := dateKey(end)
		if existing, ok := nights[date]; !ok || summary.MinutesAsleep > existing.minutes[minutesAsleep] {
			nights[date] = current
		}
	}

	return nights, nil
}

func decodeWorkouts(points []json.RawMessage) (map[string][]Workout, error) {
	byDay := map[string][]Workout{}

	for _, raw := range points {
		var point struct {
			Exercise struct {
				Interval       interval `json:"interval"`
				ExerciseType   string   `json:"exerciseType"`
				DisplayName    string   `json:"displayName"`
				ActiveDuration string   `json:"activeDuration"`
				MetricsSummary struct {
					DistanceMillimeters float64 `json:"distanceMillimeters"`
					AverageHeartRate    int64   `json:"averageHeartRateBeatsPerMinute,string"`
				} `json:"metricsSummary"`
			} `json:"exercise"`
		}
		if err := json.Unmarshal(raw, &point); err != nil {
			return nil, fmt.Errorf("decode exercise point: %w", err)
		}
		exercise := point.Exercise

		start, err := exercise.Interval.start()
		if err != nil {
			return nil, fmt.Errorf("decode exercise start: %w", err)
		}

		var duration time.Duration
		if exercise.ActiveDuration != "" {
			if duration, err = time.ParseDuration(exercise.ActiveDuration); err != nil {
				return nil, fmt.Errorf("decode exercise duration: %w", err)
			}
		}

		name := exercise.DisplayName
		if name == "" {
			name = exercise.ExerciseType
		}

		date := dateKey(start)
		byDay[date] = append(byDay[date], Workout{
			Name:       name,
			Minutes:    duration.Minutes(),
			Kilometers: exercise.MetricsSummary.DistanceMillimeters * millimetersToKm,
			HeartRate:  exercise.MetricsSummary.AverageHeartRate,
		})
	}

	return byDay, nil
}

func localTime(value string, offset string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}

	shift, err := time.ParseDuration(offset)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse UTC offset: %w", err)
	}

	return parsed.In(time.FixedZone("", int(shift.Seconds()))), nil
}
