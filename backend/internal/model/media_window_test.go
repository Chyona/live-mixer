package model

import "testing"

func TestMediaWindowList_ResolveRange(t *testing.T) {
	l := MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, URL: "https://cdn.example/w0.mp4", Ready: true},
	}
	spans, err := l.ResolveRange(1000, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 1 || spans[0].OffsetMS != 1000 || spans[0].DurMS != 4000 {
		t.Fatalf("spans=%+v", spans)
	}

	l = MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true},
		{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true},
	}
	spans, err = l.ResolveRange(590000, 610000)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("cross-window spans=%+v", spans)
	}
	if spans[0].DurMS != 10000 || spans[1].OffsetMS != 0 || spans[1].DurMS != 10000 {
		t.Fatalf("cross-window spans=%+v", spans)
	}
}

func TestMediaWindowList_ASRPendingAndUpsert(t *testing.T) {
	l := MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true},
	}
	if !l.ASRPending(100) {
		t.Fatal("expected pending")
	}
	if l.ASRPending(600000) {
		t.Fatal("expected not pending")
	}
	if l.TotalReadyMS() != 600000 {
		t.Fatalf("TotalReadyMS=%d", l.TotalReadyMS())
	}
	l = l.Upsert(MediaWindow{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true})
	if l.ReadyCount() != 2 || l.TotalReadyMS() != 1200000 {
		t.Fatalf("after upsert ReadyCount=%d TotalReadyMS=%d", l.ReadyCount(), l.TotalReadyMS())
	}
	ready := l.ReadyWindows()
	if len(ready) != 2 || ready[0].Index != 0 || ready[1].Index != 1 {
		t.Fatalf("ReadyWindows=%+v", ready)
	}
}
