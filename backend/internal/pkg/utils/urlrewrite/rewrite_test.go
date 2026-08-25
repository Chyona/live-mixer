package urlrewrite

import "testing"

func TestRewriter_Rewrite_ExactHost(t *testing.T) {
	r := New(Options{
		Exact: map[string]string{
			"arkclaw-wxbpd.tos-cn-shanghai.volces.com": "arkclaw-wxbpd.tos-cn-shanghai.ivolces.com",
		},
	})

	raw := "https://arkclaw-wxbpd.tos-cn-shanghai.volces.com/path/to/file.mp4?foo=bar"
	got, ok := r.Rewrite(raw)
	if !ok {
		t.Fatal("Rewrite() ok = false, want true")
	}
	want := "https://arkclaw-wxbpd.tos-cn-shanghai.ivolces.com/path/to/file.mp4?foo=bar"
	if got != want {
		t.Fatalf("Rewrite() = %q, want %q", got, want)
	}
}

func TestRewriter_Rewrite_WithPort(t *testing.T) {
	r := New(Options{
		Exact: map[string]string{
			"bucket.tos-cn-shanghai.volces.com": "bucket.tos-cn-shanghai.ivolces.com",
		},
	})

	got, ok := r.Rewrite("https://bucket.tos-cn-shanghai.volces.com:443/file.mp4")
	if !ok {
		t.Fatal("Rewrite() ok = false, want true")
	}
	if got != "https://bucket.tos-cn-shanghai.ivolces.com:443/file.mp4" {
		t.Fatalf("Rewrite() = %q, want port preserved", got)
	}
}

func TestRewriter_Rewrite_NoMatch(t *testing.T) {
	r := New(Options{
		Exact: map[string]string{
			"bucket.tos-cn-shanghai.volces.com": "bucket.tos-cn-shanghai.ivolces.com",
		},
	})

	raw := "https://example.com/file.mp4"
	got, ok := r.Rewrite(raw)
	if ok {
		t.Fatalf("Rewrite() ok = true, want false; got %q", got)
	}
	if got != raw {
		t.Fatalf("Rewrite() = %q, want unchanged %q", got, raw)
	}
}

func TestRewriter_Empty(t *testing.T) {
	if !New(Options{}).Empty() {
		t.Fatal("empty options should produce Empty rewriter")
	}
	if !(*Rewriter)(nil).Empty() {
		t.Fatal("nil rewriter should be Empty")
	}

	raw := "https://example.com/a.mp4"
	got, ok := New(Options{}).Rewrite(raw)
	if ok || got != raw {
		t.Fatalf("empty rewriter should not rewrite: got %q, ok=%v", got, ok)
	}
}
