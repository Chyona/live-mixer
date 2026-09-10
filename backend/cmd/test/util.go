package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func findRepoRoot() string {
	_, self, _, ok := runtime.Caller(0)
	if ok {
		root := filepath.Clean(filepath.Join(filepath.Dir(self), "..", ".."))
		if fileExists(filepath.Join(root, "go.mod")) {
			return root
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if fileExists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// HTTP API 路径前缀（webserver 挂载在 /openapi/live-mixer）。
var httpAPIPrefix = "/openapi/live-mixer"

func setHTTPAPIPrefix(prefix string) {
	p := strings.TrimSpace(prefix)
	if p == "" {
		p = "/openapi/live-mixer"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	httpAPIPrefix = strings.TrimRight(p, "/")
}

// apiURL 拼接 base + API 前缀 + path。
// path 形如 "/v1/auth/login"；若 base 已含前缀则不再重复。
func apiURL(base, path string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	path = "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	prefix := strings.TrimRight(httpAPIPrefix, "/")
	if prefix == "" {
		return base + path
	}
	if strings.HasSuffix(base, prefix) {
		return base + path
	}
	return base + prefix + path
}

func loadDotEnv(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, val)
	}
	return sc.Err()
}

func mustWriteJSON(path string, v any) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "序列化 JSON 失败: %v\n", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "写入 JSON 失败: %s: %v\n", path, err)
		os.Exit(1)
	}
}
