package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/llm"
)

// captionMockReply 单次断句调用应答；content 为 LLM 返回原文，err 为返回错误。
type captionMockReply struct {
	content string
	err     error
}

// captionMockLLM 断句测试用的 LLM 桩：按调用序取应答（用尽后重复最后一个），
// 记录调用次数、最近一次 messages 与并发峰值。
type captionMockLLM struct {
	mu      sync.Mutex
	replies []captionMockReply
	// echo 为真时忽略 replies：从用户消息取出原文，按中点报一个切点位置，
	// 使应答与调用顺序无关（并发用例需要）。
	echo     bool
	calls    int
	active   int
	peak     int
	hold     time.Duration
	lastMsgs []llm.ChatMessage
}

func (m *captionMockLLM) Chat(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.reply(ctx, messages)
}

func (m *captionMockLLM) ChatStructured(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.reply(ctx, messages)
}

func (m *captionMockLLM) ChatThinking(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	return m.reply(ctx, messages)
}

func (m *captionMockLLM) reply(ctx context.Context, messages []llm.ChatMessage) (string, error) {
	m.mu.Lock()
	m.calls++
	idx := m.calls - 1
	if !m.echo && len(m.replies) == 0 {
		m.mu.Unlock()
		return "", errors.New("captionMockLLM 未配置应答")
	}
	if idx >= len(m.replies) {
		idx = len(m.replies) - 1
	}
	var reply captionMockReply
	if !m.echo {
		reply = m.replies[idx]
	}
	m.lastMsgs = messages
	m.active++
	if m.active > m.peak {
		m.peak = m.active
	}
	hold := m.hold
	echo := m.echo
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.active--
		m.mu.Unlock()
	}()

	if hold > 0 {
		select {
		case <-time.After(hold):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if echo {
		return echoReply(messages), nil
	}
	return reply.content, reply.err
}

// echoReply 从用户消息取出原文，按中点报一个切点位置，供与调用顺序无关的用例使用。
// 位置口径同 asr.SplitLinesByCuts：前半段最后一个字的 0 基下标。
func echoReply(messages []llm.ChatMessage) string {
	text := ""
	for _, msg := range messages {
		if msg.Role == "user" {
			text = strings.TrimPrefix(msg.Content, "文本：")
		}
	}
	runes := []rune(text)
	cuts := []int{}
	if len(runes) >= 2 {
		cuts = append(cuts, len(runes)/2-1)
	}
	payload, _ := json.Marshal(map[string][]int{"cuts": cuts})
	return string(payload)
}

// cutsReply 构造「按中点切一刀」的应答：每个输入必然切出两行，能通过位置校验。
func cutsReply(texts ...string) []captionMockReply {
	out := make([]captionMockReply, 0, len(texts))
	for _, text := range texts {
		out = append(out, captionMockReply{content: echoReply([]llm.ChatMessage{{Role: "user", Content: "文本：" + text}})})
	}
	return out
}

func (m *captionMockLLM) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func (m *captionMockLLM) peakConcurrency() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.peak
}

func TestCaptionSegmentPrompt_RendersMaxRunesAndEscapedPercent(t *testing.T) {
	prompt := CaptionSegmentPrompt(12)
	if !strings.Contains(prompt, "12 个字") {
		t.Errorf("提示词未渲染行宽上限: %q", prompt)
	}
	if strings.Contains(prompt, "%%") {
		t.Error("提示词里的 %% 未被转义为 %%，模型看到的会是 3.14%%")
	}
	if !strings.Contains(prompt, "3.14%") {
		t.Errorf("提示词缺少数字与百分号示例: %q", prompt)
	}
	if !strings.Contains(prompt, `{"cuts"`) {
		t.Errorf("提示词未要求只输出切点位置: %q", prompt)
	}
	if !strings.Contains(prompt, "0 开始计") {
		t.Errorf("提示词未说明位置口径（0 基）: %q", prompt)
	}
}

func TestCaptionSegmenter_NilClientReturnsNil(t *testing.T) {
	var nilSeg *CaptionSegmenter
	if got := nilSeg.SegmentTexts(context.Background(), []string{"最直接的解决方法是升级到修复该问题的版本"}); got != nil {
		t.Errorf("nil 接收者应返回 nil，实际 %q", got)
	}

	seg := &CaptionSegmenter{}
	if got := seg.SegmentTexts(context.Background(), []string{"最直接的解决方法是升级到修复该问题的版本"}); got != nil {
		t.Errorf("未配置 Client 应返回 nil，实际 %q", got)
	}
}

func TestCaptionSegmenter_SkipsClausesWithinMaxRunes(t *testing.T) {
	mock := &captionMockLLM{echo: true}
	seg := &CaptionSegmenter{Client: mock}

	short := "今天天气不错我们聊聊天吧"
	if n := utf8.RuneCountInString(short); n != asr.MaxCaptionRunes {
		t.Fatalf("用例前提不成立：短文本 %d 字，期望 %d 字", n, asr.MaxCaptionRunes)
	}
	// 带标点的整段：标点断句后每句都在行宽内，同样不该调用模型。
	punctuated := "好的，就这样吧，明天见。"

	got := seg.SegmentTexts(context.Background(), []string{short, punctuated, "", "  "})
	if len(got) != 4 {
		t.Fatalf("结果长度 = %d, want 4", len(got))
	}
	for i, item := range got {
		if item != nil {
			t.Errorf("got[%d] = %q, 期望 nil（未超宽不应调用模型）", i, item)
		}
	}
	if mock.callCount() != 0 {
		t.Errorf("调用次数 = %d, want 0（未超宽不应调用模型）", mock.callCount())
	}
}

func TestCaptionSegmenter_UsesLLMPositionsWhenValid(t *testing.T) {
	text := "最直接的解决方法是升级到修复该问题的版本"
	mock := &captionMockLLM{replies: []captionMockReply{{content: `{"cuts": [8]}`}}}
	seg := &CaptionSegmenter{Client: mock, Model: "test-model"}

	got := seg.SegmentTexts(context.Background(), []string{text})
	want := []string{"最直接的解决方法是", "升级到修复该问题的版本"}
	if len(got) != 1 || strings.Join(got[0], "|") != strings.Join(want, "|") {
		t.Fatalf("got = %q, want %q", got, want)
	}

	// 系统提示词应为断句提示词，用户消息带上待断句原文（位置由提示词定口径）。
	if len(mock.lastMsgs) != 2 {
		t.Fatalf("messages 数 = %d, want 2", len(mock.lastMsgs))
	}
	if mock.lastMsgs[0].Role != "system" || !strings.Contains(mock.lastMsgs[0].Content, "断句") {
		t.Errorf("system 消息不是断句提示词: %+v", mock.lastMsgs[0])
	}
	if !strings.HasSuffix(mock.lastMsgs[1].Content, text) {
		t.Errorf("user 消息未带上原文: %q", mock.lastMsgs[1].Content)
	}
}

func TestCaptionSegmenter_PunctuationFirstOnlyAsksForLongClauses(t *testing.T) {
	// 标点优先：已按标点切好的短子句直接用，只有超长子句才问模型——问的也只是那个子句。
	clause := "今天我们来聊一聊人工智能在医疗领域的应用"
	text := "好的，" + clause + "。"
	mock := &captionMockLLM{echo: true}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if mock.callCount() != 1 {
		t.Fatalf("调用次数 = %d, want 1（只有超长子句需要模型）", mock.callCount())
	}
	if len(got) != 1 || len(got[0]) < 2 || got[0][0] != "好的" {
		t.Fatalf("got = %q, want 以「好的」开头且不止一行", got)
	}
	if !strings.HasSuffix(mock.lastMsgs[1].Content, clause) {
		t.Errorf("user 消息应只带超长子句（不含标点）: %q", mock.lastMsgs[1].Content)
	}
	if strings.Join(got[0][1:], "") != clause {
		t.Errorf("子句内容被改动: %q != %q", strings.Join(got[0][1:], ""), clause)
	}
}

func TestCaptionSegmenter_FallsBackPerClause(t *testing.T) {
	// 两个超长子句：第一个给了位置、第二个没给 → 只回退第二句，第一句仍用模型结果。
	first := "最直接的解决方法是升级到修复该问题的版本"
	second := "这个颜色特别好看而且它的面料非常柔软穿起来很舒服"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"cuts": [8]}`},
		{content: `{"cuts": []}`},
	}}
	seg := &CaptionSegmenter{Client: mock, Concurrency: 1}

	got := seg.SegmentTexts(context.Background(), []string{first + "，" + second})
	if mock.callCount() != 2 {
		t.Fatalf("调用次数 = %d, want 2（每个超长子句一次）", mock.callCount())
	}
	if len(got) != 1 || len(got[0]) < 3 {
		t.Fatalf("got = %q, want 至少三行", got)
	}
	if got[0][0] != "最直接的解决方法是" || got[0][1] != "升级到修复该问题的版本" {
		t.Errorf("第一句应使用模型位置: %q", got[0][:2])
	}
	if rest := strings.Join(got[0][2:], ""); rest != second {
		t.Errorf("第二句回退规则折行后内容被改动: %q != %q", rest, second)
	}
	for i, line := range got[0] {
		if n := utf8.RuneCountInString(line); n > asr.MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, asr.MaxCaptionRunes)
		}
	}
}

func TestCaptionSegmenter_ReturnsNilWhenNoClauseYieldedPositions(t *testing.T) {
	// 模型没给出任何位置（或位置不可用）时，该条为 nil：调用方按规则折行，与未启用本层一致。
	tests := []struct {
		name    string
		content string
	}{
		{name: "位置数组为空", content: `{"cuts": []}`},
		{name: "位置越界", content: `{"cuts": [999]}`},
		{name: "非 JSON", content: "我建议切在这里"},
		{name: "空内容", content: "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &captionMockLLM{replies: []captionMockReply{{content: tt.content}}}
			seg := &CaptionSegmenter{Client: mock}

			got := seg.SegmentTexts(context.Background(), []string{"最直接的解决方法是升级到修复该问题的版本"})
			if len(got) != 1 {
				t.Fatalf("结果长度 = %d, want 1", len(got))
			}
			if got[0] != nil {
				t.Fatalf("应回退规则折行（nil），实际 %q", got[0])
			}
		})
	}
}

func TestCaptionSegmenter_SnapsCutInsideToken(t *testing.T) {
	// 位置落在英文词中间：不判死回退，而是吸附到最近的合法词边界（正文一字不动）。
	text := "我们聊 ChatGPT 可以吗"
	mock := &captionMockLLM{replies: []captionMockReply{{content: `{"cuts": [6]}`}}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("应吸附成两行，实际 %q", got)
	}
	if got[0][0] != "我们聊" || got[0][1] != "ChatGPT 可以吗" {
		t.Fatalf("got = %q, want [我们聊 ChatGPT 可以吗]", got[0])
	}
}

func TestCaptionSegmenter_DedupesIdenticalClauses(t *testing.T) {
	// 同一小句在两个切片里重复出现：只调一次模型，两条结果一致。
	clause := "这个颜色特别好看而且它的面料非常柔软穿起来很舒服"
	mock := &captionMockLLM{echo: true}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{clause, clause})
	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1（小句去重）", mock.callCount())
	}
	if len(got) != 2 || len(got[0]) == 0 || len(got[1]) == 0 {
		t.Fatalf("got = %q, 期望两条都有行", got)
	}
	if strings.Join(got[0], "|") != strings.Join(got[1], "|") {
		t.Errorf("同一小句的两条结果不一致: %q vs %q", got[0], got[1])
	}
}

func TestCaptionSegmenter_TimeoutDoesNotRetry(t *testing.T) {
	mock := &captionMockLLM{replies: cutsReply("最直接的解决方法是升级到修复该问题的版本"), hold: 2 * time.Second}
	seg := &CaptionSegmenter{Client: mock, Timeout: 20 * time.Millisecond}

	start := time.Now()
	got := seg.SegmentTexts(context.Background(), []string{"最直接的解决方法是升级到修复该问题的版本"})
	elapsed := time.Since(start)

	if len(got) != 1 || got[0] != nil {
		t.Fatalf("超时应回退规则折行（nil），实际 %q", got)
	}
	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1（超时不重试）", mock.callCount())
	}
	if elapsed > time.Second {
		t.Errorf("耗时 %v，超时预算未生效", elapsed)
	}
}

func TestCaptionSegmenter_RetriesTransientErrorThenSucceeds(t *testing.T) {
	text := "最直接的解决方法是升级到修复该问题的版本"
	replies := append([]captionMockReply{
		{err: &net.DNSError{Err: "temporary failure", IsTemporary: true}},
	}, cutsReply(text)...)
	mock := &captionMockLLM{replies: replies}
	seg := &CaptionSegmenter{Client: mock, Timeout: 5 * time.Second}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || len(got[0]) == 0 {
		t.Fatalf("重试成功应得到断行结果: %q", got)
	}
	if mock.callCount() != 2 {
		t.Errorf("调用次数 = %d, want 2", mock.callCount())
	}
}

func TestCaptionSegmenter_DoesNotRetryApplicationError(t *testing.T) {
	mock := &captionMockLLM{replies: []captionMockReply{{err: errors.New("请求被拒绝")}}}
	seg := &CaptionSegmenter{Client: mock, Timeout: 5 * time.Second}

	got := seg.SegmentTexts(context.Background(), []string{"最直接的解决方法是升级到修复该问题的版本"})
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("失败应回退规则折行（nil），实际 %q", got)
	}
	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1（非瞬时错误不重试）", mock.callCount())
	}
}

func TestCaptionSegmenter_CacheReusesResult(t *testing.T) {
	text := "最直接的解决方法是升级到修复该问题的版本"
	mock := &captionMockLLM{replies: []captionMockReply{{content: `{"cuts": [8]}`}}}
	seg := &CaptionSegmenter{Client: mock}

	first := seg.SegmentTexts(context.Background(), []string{text})
	second := seg.SegmentTexts(context.Background(), []string{text})

	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1（第二次应命中缓存）", mock.callCount())
	}
	if strings.Join(first[0], "|") != strings.Join(second[0], "|") {
		t.Errorf("缓存前后结果不一致: %q vs %q", first[0], second[0])
	}
}

func TestCaptionSegmenter_CacheKeyIncludesRunesAndModel(t *testing.T) {
	// 行宽与模型都是缓存 key 的成员：模型换代或行宽常量变化时旧缓存必须失效。
	text := "最直接的解决方法是升级到修复该问题的版本"
	a := &CaptionSegmenter{Model: "model-a"}
	b := &CaptionSegmenter{Model: "model-b"}
	if a.cacheKey(text) == b.cacheKey(text) {
		t.Error("模型不同应产生不同缓存 key")
	}
	if a.cacheKey(text) == a.cacheKey(text+"。") {
		t.Error("文案不同应产生不同缓存 key")
	}
}

func TestCaptionSegmenter_RespectsConcurrencyLimit(t *testing.T) {
	const clips = 6
	texts := make([]string, clips)
	for i := range texts {
		texts[i] = fmt.Sprintf("这是第%d条待断句的切片文案内容用于测试", i)
	}
	mock := &captionMockLLM{echo: true, hold: 30 * time.Millisecond}
	seg := &CaptionSegmenter{Client: mock, Concurrency: 2}

	got := seg.SegmentTexts(context.Background(), texts)
	if len(got) != clips {
		t.Fatalf("结果长度 = %d, want %d", len(got), clips)
	}
	for i := range got {
		if len(got[i]) == 0 {
			t.Errorf("got[%d] 为空，期望得到断行", i)
		}
	}
	if peak := mock.peakConcurrency(); peak > 2 {
		t.Errorf("并发峰值 = %d, want <= 2", peak)
	}
}

func TestCaptionSegmenter_AlignsPartialFailuresByIndex(t *testing.T) {
	okText := "最直接的解决方法是升级到修复该问题的版本"
	badText := "这款产品原价是一九九元现在直播间下单只要九八折相当于省了小两百块钱"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"cuts": [8]}`},
		{content: `{"cuts": [999]}`},
	}}
	seg := &CaptionSegmenter{Client: mock, Concurrency: 1}

	got := seg.SegmentTexts(context.Background(), []string{okText, badText})
	if len(got) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(got))
	}
	if len(got[0]) == 0 || strings.Join(got[0], "") != okText {
		t.Errorf("got[0] = %q, 期望无损切分自 %q", got[0], okText)
	}
	if got[1] != nil {
		t.Errorf("got[1] = %q, 期望 nil（失败条目按位回退）", got[1])
	}
}

// charWords 把文本按逐字词造出词级时间戳（每字 perChar 毫秒）：豆包 ASR 的实际形态。
func charWords(text string, perChar int64) []asr.Word {
	words := make([]asr.Word, 0, len([]rune(text)))
	at := int64(0)
	for _, r := range []rune(text) {
		words = append(words, asr.Word{Text: string(r), StartTime: at, EndTime: at + perChar})
		at += perChar
	}
	return words
}

func TestCaptionSegmenter_OutputSurvivesConsumerValidation(t *testing.T) {
	// 断句结果要能原样通过消费侧（draft/steps）的边界防线，并让行时间走词级对齐。
	// 行与原文不一致、或切点吸附不出来，消费侧就会退回规则折行；词级对齐一旦失败，
	// 整条切片的时间会降级为按字数均分——那正是「字幕对不齐」的来源，所以这条链路钉住。
	text := "好的，最直接的解决方法是升级到修复该问题的版本。"
	mock := &captionMockLLM{replies: []captionMockReply{{content: `{"cuts": [8]}`}}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	want := []string{"好的", "最直接的解决方法是", "升级到修复该问题的版本"}
	if len(got) != 1 || strings.Join(got[0], "|") != strings.Join(want, "|") {
		t.Fatalf("got = %q, want %q", got, want)
	}

	valid, _, err := asr.SnapCaptionLines(text, got[0], asr.MaxCaptionRunes)
	if err != nil {
		t.Fatalf("消费侧再校验不应失败: %v（%q）", err, got[0])
	}

	u := asr.Utterance{Text: text, StartTime: 0, EndTime: 2400, Words: charWords(text, 100)}
	segs, source := asr.TimedSegmentsForLinesWithSource(u, valid)
	if source != asr.CaptionTimingWords {
		t.Fatalf("时间来源 = %s, want %s（降级为均分即音字对不齐）", source, asr.CaptionTimingWords)
	}
	if len(segs) != len(want) {
		t.Fatalf("字幕条数 = %d, want %d（%q）", len(segs), len(want), segs)
	}
	// 词级时间按「行的第一个字」逐行取（100ms/字）：好的=0、最=300、升=1200。
	if segs[0].StartTime != 0 || segs[1].StartTime != 300 || segs[2].StartTime != 1200 {
		t.Errorf("行起始时间 = %d/%d/%d, want 0/300/1200", segs[0].StartTime, segs[1].StartTime, segs[2].StartTime)
	}
}

func TestCaptionLinesCache_EvictsFIFO(t *testing.T) {
	cache := newCaptionLinesCache(2)
	cache.put("a", []string{"A"})
	cache.put("b", []string{"B"})
	cache.put("c", []string{"C"})

	if _, ok := cache.get("a"); ok {
		t.Error("容量 2 时最早的键应被淘汰")
	}
	for _, key := range []string{"b", "c"} {
		if _, ok := cache.get(key); !ok {
			t.Errorf("键 %q 应仍在缓存中", key)
		}
	}

	// 重复写入同一 key 不改变容量占用。
	cache.put("b", []string{"B2"})
	lines, ok := cache.get("b")
	if !ok || lines[0] != "B" {
		t.Errorf("重复写入不应覆盖已有结果: %q, %v", lines, ok)
	}
}
