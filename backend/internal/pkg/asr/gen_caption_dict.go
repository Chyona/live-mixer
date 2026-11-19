//go:build ignore

// gen_caption_dict 从 jieba 的 dict.txt 生成 caption_dict.txt（折行分词词表）。
// 用法（源文件需自行下载 jieba，见 README.md）：
//
//	go run gen_caption_dict.go -src /path/to/jieba/dict.txt -min-freq 3 > caption_dict.txt
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func main() {
	src := flag.String("src", "", "jieba dict.txt 路径（必填）")
	minFreq := flag.Int("min-freq", 3, "保留的最低频次")
	maxRunes := flag.Int("max-runes", 6, "最长词字数")
	flag.Parse()
	if *src == "" {
		fmt.Fprintln(os.Stderr, "缺少 -src：jieba dict.txt 的路径")
		os.Exit(2)
	}

	f, err := os.Open(*src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "打开 %s：%v\n", *src, err)
		os.Exit(1)
	}
	defer f.Close()

	freqs := make(map[string]int, 400000)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		word := fields[0]
		freq, err := strconv.Atoi(fields[1])
		if err != nil || freq < *minFreq {
			continue
		}
		if n := utf8.RuneCountInString(word); n == 0 || n > *maxRunes {
			continue
		}
		if !allHan(word) {
			continue
		}
		freqs[word] = freq // dict.txt 可能有重复词条，后出现的覆盖前者（与 jieba 加载一致）
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "读 %s：%v\n", *src, err)
		os.Exit(1)
	}
	if len(freqs) == 0 {
		fmt.Fprintln(os.Stderr, "没有词条入选，检查 -src 与 -min-freq")
		os.Exit(1)
	}

	words := make([]string, 0, len(freqs))
	for w := range freqs {
		words = append(words, w)
	}
	sort.Strings(words)

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, w := range words {
		fmt.Fprintf(out, "%s %d\n", w, freqs[w])
	}
	fmt.Fprintf(os.Stderr, "写出 %d 个词条\n", len(words))
}

func allHan(s string) bool {
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return true
}
