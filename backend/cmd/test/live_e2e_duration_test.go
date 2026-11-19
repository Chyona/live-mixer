package main

import (
	"testing"
	"time"
)

func TestResolveLiveE2EDurations_Default10m(t *testing.T) {
	e2e, rec, clip, err := resolveLiveE2EDurations(10*time.Minute, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if e2e != 10*time.Minute || rec != 10*time.Minute || clip != int64(10*time.Minute/time.Millisecond) {
		t.Fatalf("got e2e=%v rec=%v clip=%d", e2e, rec, clip)
	}
}

func TestResolveLiveE2EDurations_20mAnd30m(t *testing.T) {
	for _, d := range []time.Duration{20 * time.Minute, 30 * time.Minute} {
		e2e, rec, clip, err := resolveLiveE2EDurations(d, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		if e2e != d || rec != d || clip != d.Milliseconds() {
			t.Fatalf("duration=%v got e2e=%v rec=%v clip=%d", d, e2e, rec, clip)
		}
	}
}

func TestResolveLiveE2EDurations_Overrides(t *testing.T) {
	e2e, rec, clip, err := resolveLiveE2EDurations(20*time.Minute, 25*time.Minute, 15*60*1000)
	if err != nil {
		t.Fatal(err)
	}
	if e2e != 20*time.Minute || rec != 25*time.Minute || clip != 15*60*1000 {
		t.Fatalf("got e2e=%v rec=%v clip=%d", e2e, rec, clip)
	}
}
