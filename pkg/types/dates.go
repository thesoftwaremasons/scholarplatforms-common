package types

import (
	"encoding/json"
	"time"
)

// DateOnly is a custom type for handling date-only strings (YYYY-MM-DD)
// It can parse both date-only format and full RFC3339 format
type DateOnly struct {
	time.Time
}

// UnmarshalJSON implements json.Unmarshaler for DateOnly
func (d *DateOnly) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" || s == `""` {
		return nil
	}
	// Remove quotes
	s = s[1 : len(s)-1]

	// Try date-only format first
	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		d.Time = t
		return nil
	}

	// Try RFC3339 format as fallback
	t, err = time.Parse(time.RFC3339, s)
	if err == nil {
		d.Time = t
		return nil
	}

	return err
}

// MarshalJSON implements json.Marshaler for DateOnly
func (d DateOnly) MarshalJSON() ([]byte, error) {
	if d.Time.IsZero() {
		return json.Marshal(nil)
	}
	return json.Marshal(d.Time.Format("2006-01-02"))
}

// String returns the date in YYYY-MM-DD format
func (d DateOnly) String() string {
	if d.Time.IsZero() {
		return ""
	}
	return d.Time.Format("2006-01-02")
}

// ParseDateOnly parses a date string into DateOnly
func ParseDateOnly(s string) (DateOnly, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return DateOnly{}, err
	}
	return DateOnly{Time: t}, nil
}
