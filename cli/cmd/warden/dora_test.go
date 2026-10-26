package main

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	cases := []struct {
		in      string
		want    time.Time
		wantErr bool
	}{
		{"2026-10-26", time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC), false},
		{"2026-10-26T10:00:00Z", time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC), false},
		{"2026-10-26T15:30:00+05:30", time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC), false},
		{"yesterday", time.Time{}, true},
		{"", time.Time{}, true},
	}
	for _, c := range cases {
		got, err := parseSince(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("parseSince(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			continue
		}
		if !c.wantErr && !got.Equal(c.want) {
			t.Errorf("parseSince(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
