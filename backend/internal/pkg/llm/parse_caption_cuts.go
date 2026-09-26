package llm

import (
	"encoding/json"
	"fmt"
	"math"
)

// captionCutsPayload 断句 LLM 输出的标准结构：{"cuts": [8, 17]}。
// positions / indices 也收：字段名换了，但「只报位置」这一语义没变，不该为措辞判死。
type captionCutsPayload struct {
	Cuts      json.RawMessage `json:"cuts"`
	Positions json.RawMessage `json:"positions"`
	Indices   json.RawMessage `json:"indices"`
}

// ParseCaptionCuts 从 LLM 文本中解析字幕断句的切点位置。
// 兼容：{"cuts": [...]}（亦接受 positions / indices）、裸数组、单个整数、
// markdown 代码块包裹、前后夹杂说明文字；也容忍 8.0 这类整数写法。
// 位置口径见 asr.SplitLinesByCuts：值是「切点左侧最后一个字的 0 基下标」。
// 这里只做「文本 → 位置」的语法解析：越界、去重、吸附词边界与行宽都归 asr.SplitLinesByCuts 判定——
// 与「让模型产出整行文本」相比，模型输出里已经没有任何正文，内容一致性不再依赖模型。
func ParseCaptionCuts(content string) ([]int, error) {
	content = stripMarkdownFence(content)
	if content == "" {
		return nil, fmt.Errorf("LLM 返回内容为空")
	}

	if cuts, ok := tryUnmarshalCaptionCuts(content); ok {
		return cuts, nil
	}
	if extracted := extractJSONPayload(content); extracted != "" && extracted != content {
		if cuts, ok := tryUnmarshalCaptionCuts(extracted); ok {
			return cuts, nil
		}
	}

	return nil, fmt.Errorf("无法解析 LLM 返回的断句位置: %s", truncate(content, 256))
}

// tryUnmarshalCaptionCuts 依次尝试对象字段、裸值两种形态；一个整数位置都解析不出时返回 ok=false。
func tryUnmarshalCaptionCuts(content string) ([]int, bool) {
	var payload captionCutsPayload
	if err := json.Unmarshal([]byte(content), &payload); err == nil {
		for _, raw := range []json.RawMessage{payload.Cuts, payload.Positions, payload.Indices} {
			if cuts, ok := unmarshalCutsValue(raw); ok {
				return cuts, true
			}
		}
	}
	return unmarshalCutsValue(json.RawMessage(content))
}

// unmarshalCutsValue 把一段 JSON 值解析成非空的整数位置列表：[8, 17] 与 8 两种写法都收。
func unmarshalCutsValue(raw json.RawMessage) ([]int, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, false
	}
	var cuts []int
	if err := json.Unmarshal(raw, &cuts); err == nil {
		return cuts, len(cuts) > 0
	}
	var one int
	if err := json.Unmarshal(raw, &one); err == nil {
		return []int{one}, true
	}
	// 模型偶尔把位置写成 8.0：按浮点再试一次，非整数位置直接判失败。
	var floats []float64
	if err := json.Unmarshal(raw, &floats); err == nil {
		return integralCuts(floats)
	}
	var oneFloat float64
	if err := json.Unmarshal(raw, &oneFloat); err == nil {
		return integralCuts([]float64{oneFloat})
	}
	return nil, false
}

// integralCuts 把浮点位置列表转成整数列表；位置不是整数时返回 ok=false。
func integralCuts(values []float64) ([]int, bool) {
	out := make([]int, 0, len(values))
	for _, v := range values {
		if math.Trunc(v) != v {
			return nil, false
		}
		out = append(out, int(v))
	}
	return out, len(out) > 0
}
