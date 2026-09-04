package models

import "testing"

func TestContactTimelineSourceValid(t *testing.T) {
	for s := TimelineSourceProgressSent; s <= TimelineSourcePageHit; s++ {
		if !s.Valid() {
			t.Fatalf("source %d is one the feed merges and must be valid", s)
		}
	}
	for _, s := range []ContactTimelineSource{0, -1, TimelineSourcePageHit + 1, 99} {
		if s.Valid() {
			t.Fatalf("source %d names no table and must be rejected in a cursor", s)
		}
	}
}
