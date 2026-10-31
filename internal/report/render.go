package report

import (
	"fmt"
	"strings"
)

const (
	dayLayout = "02.01"
	missing   = "—"
	rowFormat = "%-22s %9s %13s %13s\n"
)

func Render(r Report) string {
	var text strings.Builder

	fmt.Fprintf(&text, "Отчёт за %s на %s\n", r.Day.Format("02.01.2006"), r.GeneratedAt.Format("15:04"))
	fmt.Fprintf(&text, "Нагрузка с 00:00 до этого времени, сон и восстановление за ночь на %s\n", r.Day.Format(dayLayout))

	renderRows(&text, "ВОССТАНОВЛЕНИЕ", r.Recovery)

	sleepTitle := "СОН"
	if !r.SleepStart.IsZero() {
		sleepTitle = fmt.Sprintf("СОН %s–%s", r.SleepStart.Format("15:04"), r.SleepEnd.Format("15:04"))
	}
	renderRows(&text, sleepTitle, r.Sleep)

	renderRows(&text, "НАГРУЗКА", r.Activity)
	renderWorkouts(&text, r)

	note := r.Note
	if note == "" {
		note = "нет заметки"
	}
	fmt.Fprintf(&text, "\nСамочувствие: %s\n", note)

	if len(r.Unavailable) > 0 {
		fmt.Fprintf(&text, "Не удалось получить: %s\n", strings.Join(r.Unavailable, ", "))
	}

	return text.String()
}

func renderRows(text *strings.Builder, title string, rows []Row) {
	fmt.Fprintf(text, "\n"+rowFormat, title, "сегодня", "ср. 7 дн.", "ср. 28 дн.")
	for _, row := range rows {
		fmt.Fprintf(text, rowFormat,
			row.Label,
			formatValue(row.Value, row.Digits),
			formatAverage(row.Week, row.Digits),
			formatAverage(row.Month, row.Digits),
		)
	}
}

func renderWorkouts(text *strings.Builder, r Report) {
	fmt.Fprintf(text, "\nТренировки за %s:", r.Day.Format(dayLayout))
	if len(r.Workouts) == 0 {
		text.WriteString(" нет")
	}
	text.WriteString("\n")

	for _, workout := range r.Workouts {
		fmt.Fprintf(text, "  %s\n", formatWorkout(workout))
	}
	fmt.Fprintf(text, "За 7 дн.: %d, %.0f мин. За 28 дн.: %d, %.0f мин\n",
		r.WeekTotal.Count, r.WeekTotal.Minutes, r.MonthTotal.Count, r.MonthTotal.Minutes)
}

func formatWorkout(workout Workout) string {
	parts := []string{fmt.Sprintf("%s: %.0f мин", workout.Name, workout.Minutes)}
	if workout.Kilometers > 0 {
		parts = append(parts, fmt.Sprintf("%.1f км", workout.Kilometers))
	}
	if workout.HeartRate > 0 {
		parts = append(parts, fmt.Sprintf("ср. пульс %d", workout.HeartRate))
	}
	return strings.Join(parts, ", ")
}

func formatValue(value *float64, digits int) string {
	if value == nil {
		return missing
	}
	return fmt.Sprintf("%.*f", digits, *value)
}

func formatAverage(average Average, digits int) string {
	if average.Days == 0 {
		return missing
	}

	text := fmt.Sprintf("%.*f", digits, average.Value)
	if average.Days*2 < average.Window {
		return fmt.Sprintf("%s (%d дн.)", text, average.Days)
	}
	return text
}
