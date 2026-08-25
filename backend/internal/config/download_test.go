package config

import "testing"

func TestDownloadConfig_URLRewriter(t *testing.T) {
	cfg := DownloadConfig{
		HostMappings: []HostMapping{{
			From: "arkclaw-wxbpd.tos-cn-shanghai.volces.com",
			To:   "arkclaw-wxbpd.tos-cn-shanghai.ivolces.com",
		}},
	}

	r := cfg.URLRewriter()
	got, ok := r.Rewrite("https://arkclaw-wxbpd.tos-cn-shanghai.volces.com/a.mp4")
	if !ok || got != "https://arkclaw-wxbpd.tos-cn-shanghai.ivolces.com/a.mp4" {
		t.Fatalf("exact mapping = %q, ok=%v", got, ok)
	}

	got, ok = r.Rewrite("https://other-bucket.tos-cn-shanghai.volces.com/a.mp4")
	if ok {
		t.Fatalf("unmapped host should not rewrite, got %q", got)
	}
}

func TestParseRewriteRules(t *testing.T) {
	rules := parseRewriteRules("host.a.com->host.b.com,host.c.com->host.d.com")
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}
	if rules[0][0] != "host.a.com" || rules[0][1] != "host.b.com" {
		t.Fatalf("rule[0] = %#v", rules[0])
	}
	if rules[1][0] != "host.c.com" || rules[1][1] != "host.d.com" {
		t.Fatalf("rule[1] = %#v", rules[1])
	}
}

func TestLoad_DownloadEnvOverride(t *testing.T) {
	t.Setenv("APP_DOWNLOAD_HOST_MAPPINGS", "a.volces.com->a.ivolces.com,b.volces.com->b.ivolces.com")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Download.HostMappings) != 2 {
		t.Fatalf("HostMappings len = %d, want 2", len(cfg.Download.HostMappings))
	}
	if cfg.Download.HostMappings[0].To != "a.ivolces.com" {
		t.Errorf("HostMappings[0] = %#v", cfg.Download.HostMappings[0])
	}

	got, ok := cfg.Download.URLRewriter().Rewrite("https://a.volces.com/x.mp4")
	if !ok || got != "https://a.ivolces.com/x.mp4" {
		t.Fatalf("rewrite = %q, ok=%v", got, ok)
	}
}
