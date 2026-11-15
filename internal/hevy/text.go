package hevy

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	// Share links have short ids like 48nSfXwj9Tv, links copied in the app have UUIDs.
	linkPattern  = regexp.MustCompile(`https?://(?:www\.)?hevy\.com/workout/[A-Za-z0-9-]+`)
	setsPattern  = regexp.MustCompile(`(?i)(сет|set) \d+:|\d\s*(kg|кг)\s*[xх×]\s*\d`)
	tokenPattern = regexp.MustCompile(`^(\d+|[A-ZА-Я])$`)
	repsPattern  = regexp.MustCompile(`[xх×] (\d+) повтор\S*`)
)

// setTags turns the letter Hevy shows instead of a set number into the tag used in notes.
var setTags = map[string]string{
	"W": "Разминка",
	"F": "В отказ",
	"D": "Дроп",
}

var headerLabels = map[string]bool{
	"Продолжительность": true,
	"Объем":             true,
	"Объём":             true,
	"Рекорды":           true,
	"Калории":           true,
	"Расстояние":        true,
}

var noiseLines = map[string]bool{
	"Скачать":           true,
	"Копировать ссылку": true,
	"ПОДХОДЫ":           true,
	"ВЕС И ПОВТОРЕНИЯ":  true,
}

// FindLink returns the first Hevy workout link in the notes, or "".
func FindLink(notes string) string {
	return linkPattern.FindString(notes)
}

// HasSets reports whether the notes already contain sets, so the page is not needed.
func HasSets(notes string) bool {
	return setsPattern.MatchString(notes)
}

// Clean turns the text of a Russian Hevy workout page into the format of workout notes:
//
//	Жим лежа (Штанга)
//	Заметка: узкий хват
//	Сет 1: 80 kg x 12
//	Сет 2: 55 kg x 6 [В отказ]
//
// An exercise on the page is its name, then its notes, then the sets. Hevy shows
// the note of this workout and the pinned note of the exercise the same way, so
// both become a «Заметка» line.
func Clean(page string) string {
	var lines []string
	for line := range strings.Lines(page) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	// pending holds the lines since the last set or header label: the name and
	// notes of the next exercise, or plain text when no sets follow.
	var out, pending []string
	flush := func() {
		out = append(out, pending...)
		pending = nil
	}
	set := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}

		switch {
		case line == "ПОДХОДЫ":
			if len(pending) > 0 {
				out = append(out, "", pending[0])
				for _, note := range pending[1:] {
					out = append(out, "Заметка: "+note)
				}
				pending = nil
			}
			set = 0
		case noiseLines[line] || strings.HasPrefix(line, "Лучший") || strings.HasPrefix(line, "Чтобы подписаться"):
		case headerLabels[line] && next != "":
			flush()
			out = append(out, line+": "+next)
			i++
		case tokenPattern.MatchString(line) && next != "":
			flush()
			set++
			entry := "Сет " + strconv.Itoa(set) + ": " + setValue(next)
			if tag, ok := setTags[line]; ok {
				entry += " [" + tag + "]"
			}
			out = append(out, entry)
			i++
		default:
			pending = append(pending, line)
		}
	}
	flush()
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func setValue(value string) string {
	value = strings.ReplaceAll(value, " кг", " kg")
	return repsPattern.ReplaceAllString(value, "x $1")
}
