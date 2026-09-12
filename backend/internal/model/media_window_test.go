package model

import "testing"

func TestMediaWindowList_ResolveRange(t *testing.T) {
	l := WithMaster(MediaWindow{
		StartMS: 0, EndMS: 600000, DurMS: 600000, URL: "https://cdn.example/master.mp4", Ready: true,
	})
	spans, err := l.ResolveRange(1000, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].OffsetMS != 1000 || spans[0].DurMS != 4000 {
		t.Fatalf("spans=%+v", spans)
	}
	if _, err := l.ResolveRange(1000, 700000); err == nil {
		t.Fatal("expected overflow error")
	}
}

func TestMediaWindowList_ASRPending(t *testing.T) {
	l := WithMaster(MediaWindow{
		StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true,
	})
	if !l.ASRPending(100) {
		t.Fatal("expected pending")
	}
	if l.ASRPending(600000) {
		t.Fatal("expected not pending")
	}
	if l.TotalReadyMS() != 600000 {
		t.Fatalf("TotalReadyMS=%d", l.TotalReadyMS())
	}
	m, ok := l.Master()
	if !ok || m.DurMS != 600000 {
		t.Fatalf("Master=%+v ok=%v", m, ok)
	}
}
