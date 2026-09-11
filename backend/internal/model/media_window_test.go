package model

import "testing"

func TestMediaWindowList_ResolveRange(t *testing.T) {
	l := MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 600000, DurMS: 600000, Ready: true, URL: "a.mp4"},
		{Index: 1, StartMS: 600000, EndMS: 1200000, DurMS: 600000, Ready: true, URL: "b.mp4"},
	}
	spans, err := l.ResolveRange(590000, 610000)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 2 {
		t.Fatalf("spans=%d", len(spans))
	}
	if spans[0].OffsetMS != 590000 || spans[0].DurMS != 10000 {
		t.Fatalf("span0=%+v", spans[0])
	}
	if spans[1].OffsetMS != 0 || spans[1].DurMS != 10000 {
		t.Fatalf("span1=%+v", spans[1])
	}
}

func TestMediaWindowList_NextPendingASR(t *testing.T) {
	l := MediaWindowList{
		{Index: 0, StartMS: 0, EndMS: 100, DurMS: 100, Ready: true},
		{Index: 1, StartMS: 100, EndMS: 200, DurMS: 100, Ready: true},
	}
	w, ok := l.NextPendingASRWindow(100)
	if !ok || w.Index != 1 {
		t.Fatalf("got %+v ok=%v", w, ok)
	}
}
