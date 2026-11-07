package hevy

import (
	"testing"
	"time"
)

func TestTimezoneIDUsesDeviceOffset(t *testing.T) {
	tests := map[int]string{
		3 * 3600:      "Etc/GMT-3",
		-5 * 3600:     "Etc/GMT+5",
		0:             "UTC",
		5*3600 + 1800: "UTC",
	}
	for offset, want := range tests {
		moment := time.Date(2026, time.November, 7, 18, 0, 0, 0, time.FixedZone("", offset))
		if got := timezoneID(moment); got != want {
			t.Errorf("timezoneID(%d) = %s, want %s", offset, got, want)
		}
	}
}

const russianPage = `Скачать

danilpapa

31 окт. 2026 г., 18:52

2 недели не тренил

Продолжительность

1 ч 39 мин

Объем

5 630 кг

Жим лежа (Штанга)

ПОДХОДЫ

ВЕС И ПОВТОРЕНИЯ

1

80 кг x 12 повторений

Лучший объем

Лучший одноповторный максимум

2

90 кг x 6 повторений

Сгибание рук на бицепс (Штанга)

ПОДХОДЫ

ВЕС И ПОВТОРЕНИЯ

W

20 кг x 15 повторений

1

50 кг x 7 повторений

F

55 кг x 6 повторений

Подъём ног к перекладине

ПОДХОДЫ

ВЕС И ПОВТОРЕНИЯ

1

10 повторений

Чтобы подписаться на danilpapa и отслеживать свои тренировки, скачайте Hevy бесплатно.

Копировать ссылку`

func TestCleanMatchesNotesFormat(t *testing.T) {
	want := `danilpapa
31 окт. 2026 г., 18:52
2 недели не тренил
Продолжительность: 1 ч 39 мин
Объем: 5 630 кг

Жим лежа (Штанга)
Сет 1: 80 kg x 12
Сет 2: 90 kg x 6

Сгибание рук на бицепс (Штанга)
Сет 1: 20 kg x 15 [Разминка]
Сет 2: 50 kg x 7
Сет 3: 55 kg x 6 [В отказ]

Подъём ног к перекладине
Сет 1: 10 повторений`

	if got := Clean(russianPage); got != want {
		t.Fatalf("Clean() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestFindLink(t *testing.T) {
	tests := map[string]string{
		"грудь\n@hevyapp\nhttps://hevy.com/workout/48nSfXwj9Tv": "https://hevy.com/workout/48nSfXwj9Tv",
		"https://www.hevy.com/workout/abc123 потом":             "https://www.hevy.com/workout/abc123",
		"https://evil.example/hevy.com/workout/abc":             "",
		"https://hevy.com/user/danilpapa":                       "",
	}
	for notes, want := range tests {
		if got := FindLink(notes); got != want {
			t.Errorf("FindLink(%q) = %q, want %q", notes, got, want)
		}
	}
}

func TestHasSets(t *testing.T) {
	tests := map[string]bool{
		"Сет 1: 80 kg x 12":                    true,
		"жим 80 кг х 8, потом 90 на 6":         true,
		"Set 2: 90kg x 6":                      true,
		"@hevyapp\nhttps://hevy.com/workout/x": false,
		"спина трицепс":                        false,
	}
	for notes, want := range tests {
		if got := HasSets(notes); got != want {
			t.Errorf("HasSets(%q) = %v, want %v", notes, got, want)
		}
	}
}
