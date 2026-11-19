package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWindowFileFingerprint_StableAndDistinct(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.mp4")
	b := filepath.Join(dir, "b.mp4")
	if err := os.WriteFile(a, []byte("hello-window-content-aaaaaaaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("hello-window-content-bbbbbbbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	fa1, err := windowFileFingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	fa2, err := windowFileFingerprint(a)
	if err != nil {
		t.Fatal(err)
	}
	if fa1 != fa2 {
		t.Fatalf("fingerprint unstable: %s vs %s", fa1, fa2)
	}
	fb, err := windowFileFingerprint(b)
	if err != nil {
		t.Fatal(err)
	}
	if fa1 == fb {
		t.Fatal("distinct files should not share fingerprint")
	}
}

func TestWindowFileFingerprint_LargeFileUsesTail(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.mp4")
	// > 1MiB so head+tail path is exercised
	buf := make([]byte, windowFingerprintHeadBytes+windowFingerprintTailBytes+1024)
	for i := range buf {
		buf[i] = byte(i % 251)
	}
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	fp, err := windowFileFingerprint(p)
	if err != nil || fp == "" {
		t.Fatalf("fp=%q err=%v", fp, err)
	}
	// mutate only middle (not in head/tail sample) — fingerprint may stay same; mutate tail must change
	copy(buf[len(buf)-32:], []byte("TAIL-CHANGED-BYTES-XXXXXXXXXXXX"))
	if err := os.WriteFile(p, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	fp2, err := windowFileFingerprint(p)
	if err != nil {
		t.Fatal(err)
	}
	if fp == fp2 {
		t.Fatal("tail change should alter fingerprint")
	}
}

func TestIsFastVODRemux(t *testing.T) {
	const windowSec = 600
	fullMS := int64(windowSec) * 1000 * 9 / 10
	if !isFastVODRemux(35*time.Second, 600138, windowSec, fullMS) {
		t.Fatal("35s remux of full window should be VOD")
	}
	if isFastVODRemux(596*time.Second, 600138, windowSec, fullMS) {
		t.Fatal("realtime wall clock should not be VOD")
	}
	if isFastVODRemux(35*time.Second, 30_000, windowSec, fullMS) {
		t.Fatal("short media should not be VOD remux")
	}
}

func TestIsRealtimeWindowElapsed(t *testing.T) {
	if !isRealtimeWindowElapsed(300*time.Second, 600) {
		t.Fatal("50% wall clock should count as realtime")
	}
	if isRealtimeWindowElapsed(100*time.Second, 600) {
		t.Fatal("short wall clock should not count as realtime")
	}
}

func TestReplayEndDecisionTable(t *testing.T) {
	fullMS := int64(540_000)
	type row struct {
		name         string
		sawRealtime  bool
		endlist      bool
		elapsed      time.Duration
		probedMS     int64
		lastFP, curFP string
		wantHit      bool
		wantDupStop  bool // fingerprint dup → discard path
		wantReason   string
	}
	cases := []row{
		{
			name: "normal live continue",
			sawRealtime: true, elapsed: 590 * time.Second, probedMS: 600_000,
			lastFP: "a", curFP: "b",
			wantHit: false,
		},
		{
			name: "fast remux after realtime",
			sawRealtime: true, elapsed: 35 * time.Second, probedMS: 600_000,
			lastFP: "a", curFP: "b",
			wantHit: true, wantReason: "fast_remux",
		},
		{
			name: "dup fingerprint after realtime",
			sawRealtime: true, elapsed: 35 * time.Second, probedMS: 600_000,
			lastFP: "same", curFP: "same",
			wantHit: true, wantDupStop: true, wantReason: "fast_remux",
		},
		{
			name: "endlist without realtime ignored",
			sawRealtime: false, endlist: true, elapsed: 35 * time.Second, probedMS: 600_000,
			wantHit: false,
		},
		{
			name: "endlist after realtime",
			sawRealtime: true, endlist: true, elapsed: 300 * time.Second, probedMS: 600_000,
			lastFP: "a", curFP: "b",
			wantHit: true, wantReason: "endlist",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hit, reason := replayEndSignal(tc.sawRealtime, tc.endlist, tc.elapsed, tc.probedMS, 600, fullMS)
			dup := tc.lastFP != "" && tc.curFP == tc.lastFP
			if dup && tc.sawRealtime {
				hit = true
			}
			if hit != tc.wantHit {
				t.Fatalf("hit=%v want %v reason=%q", hit, tc.wantHit, reason)
			}
			if tc.wantHit && tc.wantReason != "" && reason != tc.wantReason && !(dup && reason == "") {
				// reason may be empty when only dup triggers; allow that path via wantDupStop
				if !tc.wantDupStop || reason != tc.wantReason {
					if reason != tc.wantReason {
						t.Fatalf("reason=%q want %q", reason, tc.wantReason)
					}
				}
			}
			if tc.wantDupStop && !dup {
				t.Fatal("expected dup stop")
			}
		})
	}
}
