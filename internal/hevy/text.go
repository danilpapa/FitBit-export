package hevy

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	linkPattern  = regexp.MustCompile(`https?://(?:www\.)?hevy\.com/workout/[A-Za-z0-9]+`)
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
//	Сет 1: 80 kg x 12
//	Сет 2: 55 kg x 6 [В отказ]
func Clean(page string) string {
	var lines []string
	for line := range strings.Lines(page) {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}

	var out []string
	set := 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}

		switch {
		case noiseLines[line] || strings.HasPrefix(line, "Лучший") || strings.HasPrefix(line, "Чтобы подписаться"):
		case next == "ПОДХОДЫ":
			out = append(out, "", line)
			set = 0
		case headerLabels[line] && next != "":
			out = append(out, line+": "+next)
			i++
		case tokenPattern.MatchString(line) && next != "":
			set++
			entry := "Сет " + strconv.Itoa(set) + ": " + setValue(next)
			if tag, ok := setTags[line]; ok {
				entry += " [" + tag + "]"
			}
			out = append(out, entry)
			i++
		default:
			out = append(out, line)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func setValue(value string) string {
	value = strings.ReplaceAll(value, " кг", " kg")
	return repsPattern.ReplaceAllString(value, "x $1")
}
