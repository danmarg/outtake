package lib

import (
	"testing"
	"time"
)

func TestBackoffDuration(t *testing.T) {
	start := 10 * time.Millisecond
	cases := []struct {
		attempt uint
		max     time.Duration
		want    time.Duration
	}{
		{attempt: 0, max: 0, want: 10 * time.Millisecond},
		{attempt: 1, max: 0, want: 20 * time.Millisecond},
		{attempt: 2, max: 0, want: 40 * time.Millisecond},
		{attempt: 3, max: 0, want: 80 * time.Millisecond},
		{attempt: 4, max: 50 * time.Millisecond, want: 50 * time.Millisecond},
	}

	for _, tc := range cases {
		if got := backoffDuration(start, tc.attempt, tc.max); got != tc.want {
			t.Errorf("backoffDuration(%v, %d, %v) = %v, expected %v", start, tc.attempt, tc.max, got, tc.want)
		}
	}
}
