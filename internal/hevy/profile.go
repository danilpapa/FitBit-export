package hevy

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// The profile page loads slowly: the Hevy server sometimes takes 20 seconds for the HTML.
const profileTimeout = 90 * time.Second

// The profile page shows only the newest workout to a signed-out visitor: the
// list of older ones needs an account. The card is cut to three exercises, so
// the sets come from the workout page. The card has the workout description,
// which the workout page does not show. The date sits under the user name, in
// the link to the profile next to the link to the workout.
const cardScript = `(() => {
	const a = document.querySelector("a[href^='/workout/']");
	if (!a) return {link: "", date: "", title: "", description: ""};
	const date = a.parentElement && a.parentElement.querySelector("a[href^='/user/'] p");
	const title = a.querySelector("h5");
	const description = title && title.nextElementSibling;
	return {
		link: a.href,
		date: date ? date.innerText.trim() : "",
		title: title ? title.innerText.trim() : "",
		description: description ? description.innerText.trim() : "",
	};
})()`

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	// 13 нояб. 2026 г., 18:14. Intl may put non-breaking spaces between the parts.
	datePattern = regexp.MustCompile(`^(\d{1,2})[\s\x{a0}\x{202f}]+([а-я]+)\.?[\s\x{a0}\x{202f}]+(\d{4})[\s\x{a0}\x{202f}]*г\.,?[\s\x{a0}\x{202f}]+(\d{1,2}):(\d{2})$`)
	// Вчера в 18:49.
	yesterdayPattern = regexp.MustCompile(`^Вчера в (\d{1,2}):(\d{2})$`)
	// 3 часа назад, несколько секунд назад, or через минуту when the clocks differ a little.
	relativePattern = regexp.MustCompile(`^(.{1,30} назад|через .{1,30})$`)
)

var monthPrefixes = []string{"янв", "фев", "мар", "апр", "ма", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// Card is the newest workout on a profile page.
type Card struct {
	Link        string `json:"link"`
	Date        string `json:"date"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// StartedAt is zero when the workout ended today: Hevy shows such a workout
	// only relative to now, as "3 часа назад", and EndedToday is true.
	StartedAt  time.Time `json:"-"`
	EndedToday bool      `json:"-"`
}

// Latest opens the public profile page of username and reads the card of the newest workout.
func (p *Pages) Latest(ctx context.Context, username string) (Card, error) {
	if !usernamePattern.MatchString(username) {
		return Card{}, fmt.Errorf("not a Hevy username: %q", username)
	}
	p.once.Do(p.start)
	if p.err != nil {
		return Card{}, p.err
	}

	tab, cancelTab := chromedp.NewContext(p.browser)
	defer cancelTab()
	stop := context.AfterFunc(ctx, cancelTab)
	defer stop()
	tab, cancelTimeout := context.WithTimeout(tab, profileTimeout)
	defer cancelTimeout()

	started := time.Now()
	profile := "https://hevy.com/user/" + username
	if err := p.prepare(tab); err != nil {
		return Card{}, fmt.Errorf("prepare Chrome tab: %w", err)
	}
	if _, err := chromedp.Run(tab, chromedp.Navigate(profile)); err != nil {
		return Card{}, fmt.Errorf("open %s: %w", profile, err)
	}

	for {
		card, err := chromedp.Run(tab, chromedp.Evaluate[Card](cardScript))
		if err != nil {
			return Card{}, fmt.Errorf("read %s: %w", profile, err)
		}
		text, err := chromedp.Run(tab, chromedp.Evaluate[string](textScript))
		if err != nil {
			return Card{}, fmt.Errorf("read %s: %w", profile, err)
		}

		if card.Link != "" {
			if card.StartedAt, card.EndedToday, err = ParseDate(card.Date, time.Now().In(p.location)); err != nil {
				return Card{}, fmt.Errorf("read %s: %w", profile, err)
			}
			log.Printf("hevy: read %s in %s, the newest workout: %s",
				profile, time.Since(started).Round(100*time.Millisecond), card.Date)
			return card, nil
		}
		if strings.Contains(text, "не найден") {
			return Card{}, fmt.Errorf("Hevy has no user %s", username)
		}

		select {
		case <-tab.Done():
			return Card{}, fmt.Errorf("read %s after %s: the page shows no workouts", profile, time.Since(started).Round(time.Second))
		case <-time.After(pollEvery):
		}
	}
}

// WithDescription puts the workout description from the card under the workout title.
func WithDescription(notes string, card Card) string {
	if card.Description == "" {
		return notes
	}
	line := "Описание: " + card.Description
	if card.Title != "" {
		if before, after, found := strings.Cut(notes, card.Title+"\n"); found {
			return before + card.Title + "\n" + line + "\n" + after
		}
	}
	return line + "\n" + notes
}

// ParseDate reads the date of a workout on a Russian Hevy page. The page is
// rendered in the time zone of the device clock, the one of now, and shows the
// date in one of three ways:
//
//	3 часа назад             the workout ended today; the start is unknown
//	Вчера в 18:49            it started yesterday
//	13 нояб. 2026 г., 18:14  it started earlier
func ParseDate(line string, now time.Time) (started time.Time, endedToday bool, err error) {
	line = strings.TrimSpace(line)
	if relativePattern.MatchString(line) {
		return time.Time{}, true, nil
	}
	if match := yesterdayPattern.FindStringSubmatch(line); match != nil {
		hour, _ := strconv.Atoi(match[1])
		minute, _ := strconv.Atoi(match[2])
		year, month, day := now.AddDate(0, 0, -1).Date()
		return time.Date(year, month, day, hour, minute, 0, 0, now.Location()), false, nil
	}

	match := datePattern.FindStringSubmatch(line)
	if match == nil {
		return time.Time{}, false, fmt.Errorf("unknown workout date %q", line)
	}
	month := 0
	for i, prefix := range monthPrefixes {
		if strings.HasPrefix(match[2], prefix) {
			month = i + 1
			break
		}
	}
	if month == 0 {
		return time.Time{}, false, fmt.Errorf("unknown month in workout date %q", line)
	}
	day, _ := strconv.Atoi(match[1])
	year, _ := strconv.Atoi(match[3])
	hour, _ := strconv.Atoi(match[4])
	minute, _ := strconv.Atoi(match[5])
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, now.Location()), false, nil
}

// WorkoutDate finds the date line in the notes of a workout page and reads it.
func WorkoutDate(notes string, now time.Time) (started time.Time, endedToday bool, err error) {
	for line := range strings.Lines(notes) {
		if started, endedToday, err := ParseDate(line, now); err == nil {
			return started, endedToday, nil
		}
	}
	return time.Time{}, false, errors.New("no workout date on the page")
}
