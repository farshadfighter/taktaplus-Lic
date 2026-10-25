package licformat

import (
	"encoding/json"
	"fmt"
	"time"
)

const dateLayout = "2006-01-02"

// Date is a calendar day in UTC, serialized as "YYYY-MM-DD".
type Date struct{ t time.Time }

// NewDate returns the Date for the given year, month and day.
func NewDate(year int, month time.Month, day int) Date {
	return Date{time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// ParseDate parses "YYYY-MM-DD".
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err != nil {
		return Date{}, fmt.Errorf("licformat: invalid date %q: %w", s, err)
	}
	return Date{t}, nil
}

// IsZero reports whether d is unset.
func (d Date) IsZero() bool { return d.t.IsZero() }

// String formats the date as "YYYY-MM-DD".
func (d Date) String() string { return d.t.Format(dateLayout) }

// EndOfDay is the first instant after this day; an item that expires on d is
// valid until (not including) this instant.
func (d Date) EndOfDay() time.Time { return d.t.AddDate(0, 0, 1) }

func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(d.String())
}

func (d *Date) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("licformat: date must be a string: %w", err)
	}
	if s == "" {
		*d = Date{}
		return nil
	}
	parsed, err := ParseDate(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}
