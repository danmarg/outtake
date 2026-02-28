package lib

import (
	"testing"
	"time"
)

func TestBackoffDuration(t *testing.T) {
	start := 10 * time.Millisecond
	cases := []struct {
		attempt uint
		want    time.Duration
	}{
		{attempt: 0, want: 10 * time.Millisecond},
		{attempt: 1, want: 20 * time.Millisecond},
		{attempt: 2, want: 40 * time.Millisecond},
		{attempt: 3, want: 80 * time.Millisecond},
	}

	for _, tc := range cases {
		if got := backoffDuration(start, tc.attempt); got != tc.want {
			t.Errorf("backoffDuration(%v, %d) = %v, expected %v", start, tc.attempt, got, tc.want)
		}
	}
}
