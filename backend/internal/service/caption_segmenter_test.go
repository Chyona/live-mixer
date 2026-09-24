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
	// echo 为真时忽略 replies：把用户消息里的原文原样包成单行 JSON 返回，
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

// echoReply 从用户消息取出原文并原样包成单行 JSON，供与调用顺序无关的用例使用。
func echoReply(messages []llm.ChatMessage) string {
	text := ""
	for _, msg := range messages {
		if msg.Role == "user" {
			text = strings.TrimPrefix(msg.Content, "文本：")
		}
	}
	payload, _ := json.Marshal(map[string][]string{"lines": {text}})
	return string(payload)
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

// textsReply 构造「原样返回整段」的应答：单行输入必然通过无损校验，再被硬约束切成合规行。
func textsReply(texts ...string) []captionMockReply {
	out := make([]captionMockReply, 0, len(texts))
	for _, text := range texts {
		payload, _ := json.Marshal(map[string][]string{"lines": {text}})
		out = append(out, captionMockReply{content: string(payload)})
	}
	return out
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
}

func TestCaptionSegmenter_NilClientReturnsNil(t *testing.T) {
	var nilSeg *CaptionSegmenter
	if got := nilSeg.SegmentTexts(context.Background(), []string{"今天我们来聊一聊人工智能在医疗领域的应用"}); got != nil {
		t.Errorf("nil 接收者应返回 nil，实际 %q", got)
	}

	seg := &CaptionSegmenter{}
	if got := seg.SegmentTexts(context.Background(), []string{"今天我们来聊一聊人工智能在医疗领域的应用"}); got != nil {
		t.Errorf("未配置 Client 应返回 nil，实际 %q", got)
	}
}

func TestCaptionSegmenter_SkipsTextsWithinMaxRunes(t *testing.T) {
	mock := &captionMockLLM{echo: true}
	seg := &CaptionSegmenter{Client: mock}

	short := "今天天气不错我们聊聊天吧"
	long := "今天我们来聊一聊人工智能在医疗领域的应用"
	if n := utf8.RuneCountInString(short); n != asr.MaxCaptionRunes {
		t.Fatalf("用例前提不成立：短文本 %d 字，期望 %d 字", n, asr.MaxCaptionRunes)
	}

	got := seg.SegmentTexts(context.Background(), []string{short, "", "  "})
	if len(got) != 3 {
		t.Fatalf("结果长度 = %d, want 3", len(got))
	}
	for i, item := range got {
		if item != nil {
			t.Errorf("got[%d] = %q, 期望 nil（未超宽不应调用模型）", i, item)
		}
	}
	if mock.callCount() != 0 {
		t.Errorf("调用次数 = %d, want 0（未超宽不应调用模型）", mock.callCount())
	}

	// 超宽才调用，且结果与输入等长对齐。
	got = seg.SegmentTexts(context.Background(), []string{short, long, ""})
	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1", mock.callCount())
	}
	if got[0] != nil || got[2] != nil {
		t.Errorf("未超宽/空文本应为 nil: %q", got)
	}
	if len(got[1]) == 0 {
		t.Fatalf("超宽文本应得到断行结果: %q", got[1])
	}
	for i, line := range got[1] {
		if n := utf8.RuneCountInString(line); n > asr.MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, asr.MaxCaptionRunes)
		}
	}
}

func TestCaptionSegmenter_UsesLLMLinesWhenValid(t *testing.T) {
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["今天我们来聊一聊", "人工智能在医疗领域的应用"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock, Model: "test-model"}

	got := seg.SegmentTexts(context.Background(), []string{"今天我们来聊一聊人工智能在医疗领域的应用"})
	want := []string{"今天我们来聊一聊", "人工智能在医疗领域的应用"}
	if len(got) != 1 || strings.Join(got[0], "|") != strings.Join(want, "|") {
		t.Fatalf("got = %q, want %q", got, want)
	}

	// 系统提示词应为断句提示词，用户消息带上待断句原文（并带行宽上限）。
	if len(mock.lastMsgs) != 2 {
		t.Fatalf("messages 数 = %d, want 2", len(mock.lastMsgs))
	}
	if mock.lastMsgs[0].Role != "system" || !strings.Contains(mock.lastMsgs[0].Content, "断句") {
		t.Errorf("system 消息不是断句提示词: %+v", mock.lastMsgs[0])
	}
	if !strings.HasSuffix(mock.lastMsgs[1].Content, "今天我们来聊一聊人工智能在医疗领域的应用") {
		t.Errorf("user 消息未带上原文: %q", mock.lastMsgs[1].Content)
	}
}

func TestCaptionSegmenter_FallsBackOnInvalidOutput(t *testing.T) {
	text := "我们聊 ChatGPT 可以吗"
	tests := []struct {
		name    string
		content string
	}{
		{name: "改写原文", content: `{"lines": ["我们聊 ChatGPT", "很好用"]}`},
		{name: "漏字", content: `{"lines": ["我们聊", "可以吗"]}`},
		{name: "空行+改写", content: `{"lines": ["我们聊 ChatGPT", "", "很好用"]}`},
		{name: "非 JSON", content: "第一行\n第二行"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &captionMockLLM{replies: []captionMockReply{{content: tt.content}}}
			seg := &CaptionSegmenter{Client: mock}

			got := seg.SegmentTexts(context.Background(), []string{text})
			if len(got) != 1 {
				t.Fatalf("结果长度 = %d, want 1", len(got))
			}
			if got[0] != nil {
				t.Fatalf("校验不通过应回退规则折行（nil），实际 %q", got[0])
			}
		})
	}
}

func TestCaptionSegmenter_SnapsCutOutOfToken(t *testing.T) {
	// 模型把英文词从中间切开：不再整条判死回退规则折行，而是把切点吸附到词边界（内容一字不动，
	// 模型给的行数保留）。
	text := "我们聊 ChatGPT 可以吗"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["我们聊 ChatGP", "T 可以吗"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("应吸附成两行，实际 %q", got)
	}
	if got[0][0] != "我们聊 ChatGPT" || got[0][1] != "可以吗" {
		t.Fatalf("got = %q, want [我们聊 ChatGPT 可以吗]", got[0])
	}
}

func TestCaptionSegmenter_FallsBackWhenUnsnappable(t *testing.T) {
	// 模型给的行数在合法词边界上排不出来（三个 12 字英文原子共 38 字，3 行装不下）→ 回退规则折行。
	text := "AAAAAAAAAAAA BBBBBBBBBBBB CCCCCCCCCCCC"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["AAAAAAAAAAAA", " BBBBBBBBBBBB", " CCCCCCCCCCCC"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("吸附不出应回退规则折行（nil），实际 %q", got)
	}
}

func TestCaptionSegmenter_AcceptsDroppedBoundaryPunctuation(t *testing.T) {
	// 实测模型行为：行边界的标点/空白会被直接丢掉，内容一字不少即可采纳（产品规则本就要剥行首尾标点）。
	text := "这款产品原价是￥199，现在直播间下单只要98折，相当于省了小两百块钱。"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	want := []string{"这款产品原价是￥199", "现在直播间下单只要98折", "相当于省了小两百块钱"}
	if len(got) != 1 || len(got[0]) != len(want) {
		t.Fatalf("got = %q, want %q", got, want)
	}
	for i := range want {
		if got[0][i] != want[i] {
			t.Errorf("lines[%d] = %q, want %q", i, got[0][i], want[i])
		}
	}
}

func TestCaptionSegmenter_IgnoresEmptyLineNoise(t *testing.T) {
	// 模型偶尔多给一个空行：ParseCaptionLines 丢掉空行，其余行内容合法时照常采纳。
	text := "我们聊 ChatGPT 可以吗"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["我们聊 ChatGPT", "", "可以吗"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || len(got[0]) != 2 || got[0][0] != "我们聊 ChatGPT" || got[0][1] != "可以吗" {
		t.Fatalf("got = %q, want [我们聊 ChatGPT 可以吗]", got)
	}
}

func TestCaptionSegmenter_ResplitsOverlongLine(t *testing.T) {
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: fmt.Sprintf(`{"lines": [%q]}`, text)},
	}}
	seg := &CaptionSegmenter{Client: mock}

	got := seg.SegmentTexts(context.Background(), []string{text})
	if len(got) != 1 || len(got[0]) < 2 {
		t.Fatalf("超长行应被再切分，实际 %q", got)
	}
	for i, line := range got[0] {
		if n := utf8.RuneCountInString(line); n > asr.MaxCaptionRunes {
			t.Errorf("lines[%d] = %q 长度 %d > %d", i, line, n, asr.MaxCaptionRunes)
		}
	}
}

func TestCaptionSegmenter_TimeoutDoesNotRetry(t *testing.T) {
	mock := &captionMockLLM{replies: textsReply("今天我们来聊一聊人工智能在医疗领域的应用"), hold: 2 * time.Second}
	seg := &CaptionSegmenter{Client: mock, Timeout: 20 * time.Millisecond}

	start := time.Now()
	got := seg.SegmentTexts(context.Background(), []string{"今天我们来聊一聊人工智能在医疗领域的应用"})
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
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	replies := append([]captionMockReply{
		{err: &net.DNSError{Err: "temporary failure", IsTemporary: true}},
	}, textsReply(text)...)
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

	got := seg.SegmentTexts(context.Background(), []string{"今天我们来聊一聊人工智能在医疗领域的应用"})
	if len(got) != 1 || got[0] != nil {
		t.Fatalf("失败应回退规则折行（nil），实际 %q", got)
	}
	if mock.callCount() != 1 {
		t.Errorf("调用次数 = %d, want 1（非瞬时错误不重试）", mock.callCount())
	}
}

func TestCaptionSegmenter_CacheReusesResult(t *testing.T) {
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["今天我们来聊一聊", "人工智能在医疗领域的应用"]}`},
	}}
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
	text := "今天我们来聊一聊人工智能在医疗领域的应用"
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
	okText := "今天我们来聊一聊人工智能在医疗领域的应用"
	badText := "这款产品原价是199元现在下单更便宜"
	mock := &captionMockLLM{replies: []captionMockReply{
		{content: `{"lines": ["今天我们来聊一聊", "人工智能在医疗领域的应用"]}`},
		{content: `{"lines": ["完全被改写的另一段内容"]}`},
	}}
	seg := &CaptionSegmenter{Client: mock, Concurrency: 1}

	got := seg.SegmentTexts(context.Background(), []string{okText, badText})
	if len(got) != 2 {
		t.Fatalf("结果长度 = %d, want 2", len(got))
	}
	if strings.Join(got[0], "") != okText {
		t.Errorf("got[0] = %q, 期望无损切分自 %q", got[0], okText)
	}
	if got[1] != nil {
		t.Errorf("got[1] = %q, 期望 nil（失败条目按位回退）", got[1])
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
