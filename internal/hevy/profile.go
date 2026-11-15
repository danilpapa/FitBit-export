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
// which the workout page does not show.
const cardScript = `(() => {
	const a = document.querySelector("a[href^='/workout/']");
	if (!a) return {link: "", title: "", description: ""};
	const title = a.querySelector("h5");
	const description = title && title.nextElementSibling;
	return {
		link: a.href,
		title: title ? title.innerText.trim() : "",
		description: description ? description.innerText.trim() : "",
	};
})()`

var (
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	// 13 нояб. 2026 г., 18:14. Intl may put non-breaking spaces between the parts.
	datePattern = regexp.MustCompile(`(\d{1,2})[\s\x{a0}\x{202f}]+([а-я]+)\.?[\s\x{a0}\x{202f}]+(\d{4})[\s\x{a0}\x{202f}]*г\.,?[\s\x{a0}\x{202f}]+(\d{1,2}):(\d{2})`)
)

var monthPrefixes = []string{"янв", "фев", "мар", "апр", "ма", "июн", "июл", "авг", "сен", "окт", "ноя", "дек"}

// Card is the newest workout on a profile page.
type Card struct {
	Link        string    `json:"link"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	StartedAt   time.Time `json:"-"`
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
			if card.StartedAt, err = ParseDate(text, p.location); err != nil {
				return Card{}, fmt.Errorf("read %s: %w", profile, err)
			}
			log.Printf("hevy: read %s in %s, the newest workout started %s",
				profile, time.Since(started).Round(100*time.Millisecond), card.StartedAt.Format("2006-01-02 15:04"))
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

// ParseDate reads the first date of a Russian Hevy page, such as 13 нояб. 2026 г., 18:14.
// Pages are rendered in the time zone of the device clock, so the date is in location.
func ParseDate(text string, location *time.Location) (time.Time, error) {
	match := datePattern.FindStringSubmatch(text)
	if match == nil {
		return time.Time{}, errors.New("no workout date on the page")
	}
	month := 0
	for i, prefix := range monthPrefixes {
		if strings.HasPrefix(match[2], prefix) {
			month = i + 1
			break
		}
	}
	if month == 0 {
		return time.Time{}, fmt.Errorf("unknown month %q", match[2])
	}
	day, _ := strconv.Atoi(match[1])
	year, _ := strconv.Atoi(match[3])
	hour, _ := strconv.Atoi(match[4])
	minute, _ := strconv.Atoi(match[5])
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, location), nil
}
