package service

import (
	"strings"
	"testing"
	"unicode/utf8"

	"live-mixer/internal/pkg/asr"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func utt(speaker, text string, start, end int64) asr.Utterance {
	words := make([]asr.Word, 0, utf8.RuneCountInString(text))
	n := int64(utf8.RuneCountInString(text))
	if n == 0 {
		n = 1
	}
	step := (end - start) / n
	if step <= 0 {
		step = 1
	}
	t := start
	for _, r := range text {
		ws := t
		we := t + step
		if we > end {
			we = end
		}
		words = append(words, asr.Word{Text: string(r), StartTime: ws, EndTime: we})
		t = we
	}
	return asr.Utterance{
		Speaker:   speaker,
		Text:      text,
		StartTime: start,
		EndTime:   end,
		Words:     words,
	}
}

func TestBuildASRParagraphsByMinGap_MergeSameSpeakerSmallGap(t *testing.T) {
	in := []asr.Utterance{
		utt("1", "你好", 0, 100),
		utt("1", "世界", 120, 200),
	}
	paras, warns := BuildASRParagraphsByMinGap(in, 200, nil)
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %+v", warns)
	}
	if len(paras) != 1 {
		t.Fatalf("paras=%d want 1", len(paras))
	}
	if paras[0].Text != "你好世界" {
		t.Fatalf("text=%q", paras[0].Text)
	}
	if paras[0].Speaker != "1" {
		t.Fatalf("speaker=%q", paras[0].Speaker)
	}
}

func TestBuildASRParagraphsByMinGap_DifferentSpeakerNoMerge(t *testing.T) {
	in := []asr.Utterance{
		utt("1", "甲", 0, 100),
		utt("2", "乙", 110, 200),
	}
	paras, _ := BuildASRParagraphsByMinGap(in, 200, nil)
	if len(paras) != 2 {
		t.Fatalf("paras=%d want 2", len(paras))
	}
}

func TestBuildASRParagraphsByMinGap_ExceedMaxLenNoMerge(t *testing.T) {
	a := strings.Repeat("啊", 120)
	b := strings.Repeat("吧", 120)
	in := []asr.Utterance{
		utt("1", a, 0, 100),
		utt("1", b, 110, 200),
	}
	paras, _ := BuildASRParagraphsByMinGap(in, 200, nil)
	if len(paras) != 2 {
		t.Fatalf("paras=%d want 2 (merged runes would be 240)", len(paras))
	}
}

func TestBuildASRParagraphsByMinGap_PreferMinNonNegGap(t *testing.T) {
	// 三句同人：gap(0,1)=50，gap(1,2)=10 → 先合 1+2，再与 0 合（若字数允许）
	in := []asr.Utterance{
		utt("1", "一", 0, 100),
		utt("1", "二", 150, 200),
		utt("1", "三", 210, 300),
	}
	paras, _ := BuildASRParagraphsByMinGap(in, 200, nil)
	if len(paras) != 1 {
		t.Fatalf("paras=%d want 1", len(paras))
	}
	if paras[0].Text != "一二三" {
		t.Fatalf("text=%q", paras[0].Text)
	}

	// 字数限制使只能合一对：应合 gap 更小的 1+2（「二三」），留下「一」
	in2 := []asr.Utterance{
		utt("1", strings.Repeat("甲", 150), 0, 100),
		utt("1", strings.Repeat("乙", 30), 150, 200),
		utt("1", strings.Repeat("丙", 30), 210, 300),
	}
	paras2, _ := BuildASRParagraphsByMinGap(in2, 200, nil)
	if len(paras2) != 2 {
		t.Fatalf("paras2=%d want 2", len(paras2))
	}
	if utf8.RuneCountInString(paras2[0].Text) != 150 {
		t.Fatalf("first seg runes=%d want 150 (should keep 甲*)", utf8.RuneCountInString(paras2[0].Text))
	}
	if utf8.RuneCountInString(paras2[1].Text) != 60 {
		t.Fatalf("second seg runes=%d want 60 (乙+丙)", utf8.RuneCountInString(paras2[1].Text))
	}
}

func TestBuildASRParagraphsByMinGap_TieBreakSmallerIndex(t *testing.T) {
	// 两对 gap 相同：应先合左边
	in := []asr.Utterance{
		utt("1", "A", 0, 100),
		utt("1", "B", 150, 200),
		utt("1", "C", 250, 300),
	}
	paras, _ := BuildASRParagraphsByMinGap(in, 2, nil) // maxlen=2：只能合一对，且合并后再无法与第三句合
	if len(paras) != 2 {
		t.Fatalf("paras=%d want 2", len(paras))
	}
	if paras[0].Text != "AB" || paras[1].Text != "C" {
		t.Fatalf("got %q %q; want AB then C (tie → smaller i)", paras[0].Text, paras[1].Text)
	}
}

func TestBuildASRParagraphsByMinGap_EmptyAndSingle(t *testing.T) {
	paras, warns := BuildASRParagraphsByMinGap(nil, 200, nil)
	if len(paras) != 0 || len(warns) != 0 {
		t.Fatalf("empty: paras=%d warns=%d", len(paras), len(warns))
	}
	in := []asr.Utterance{utt("1", "单", 0, 50)}
	paras, warns = BuildASRParagraphsByMinGap(in, 200, nil)
	if len(paras) != 1 || paras[0].Text != "单" || len(warns) != 0 {
		t.Fatalf("single: %+v warns=%+v", paras, warns)
	}
}

func TestBuildASRParagraphsByMinGap_SameSpeakerOverlapWarnNoMerge(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	logger := zap.New(core)

	in := []asr.Utterance{
		utt("1", "前", 0, 100),
		utt("1", "后", 80, 160), // overlap gap=-20
	}
	paras, warns := BuildASRParagraphsByMinGap(in, 200, logger)
	if len(paras) != 2 {
		t.Fatalf("paras=%d want 2 (overlap must not merge)", len(paras))
	}
	if len(warns) != 1 {
		t.Fatalf("warns=%d want 1: %+v", len(warns), warns)
	}
	if warns[0].GapMs >= 0 || warns[0].Speaker != "1" {
		t.Fatalf("bad warning: %+v", warns[0])
	}
	if logs.FilterMessageSnippet("同一说话人相邻语句时间重叠").Len() == 0 {
		t.Fatalf("expected strong warn log, got %d entries", logs.Len())
	}
}

func TestBuildASRParagraphsByMinGap_DifferentSpeakerOverlapNoWarn(t *testing.T) {
	in := []asr.Utterance{
		utt("1", "甲", 0, 100),
		utt("2", "乙", 50, 150),
	}
	paras, warns := BuildASRParagraphsByMinGap(in, 200, nil)
	if len(paras) != 2 {
		t.Fatalf("paras=%d", len(paras))
	}
	if len(warns) != 0 {
		t.Fatalf("different speaker overlap should not warn: %+v", warns)
	}
}

func TestBuildASRParagraphsByMinGap_DefaultMaxLen(t *testing.T) {
	in := []asr.Utterance{
		utt("1", "短", 0, 10),
		utt("1", "句", 20, 30),
	}
	paras, _ := BuildASRParagraphsByMinGap(in, 0, nil)
	if len(paras) != 1 {
		t.Fatalf("default maxlen should merge: paras=%d", len(paras))
	}
}
