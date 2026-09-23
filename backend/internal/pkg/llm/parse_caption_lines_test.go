package llm

import (
	"reflect"
	"testing"
)

func TestParseCaptionLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
		wantErr bool
	}{
		{
			name:    "标准对象",
			content: `{"lines": ["今天我们来聊一聊", "人工智能在医疗领域的应用"]}`,
			want:    []string{"今天我们来聊一聊", "人工智能在医疗领域的应用"},
		},
		{
			name:    "裸数组",
			content: `["今天我们来聊一聊", "人工智能在医疗领域的应用"]`,
			want:    []string{"今天我们来聊一聊", "人工智能在医疗领域的应用"},
		},
		{
			name:    "markdown 代码块包裹",
			content: "```json\n{\"lines\": [\"这款产品原价是￥199\", \"现在直播间下单只要98折\"]}\n```",
			want:    []string{"这款产品原价是￥199", "现在直播间下单只要98折"},
		},
		{
			name:    "夹杂前后说明文字",
			content: "好的，断句结果如下：\n{\"lines\": [\"美联储加息\", \"对科创板影响有限\"]}\n希望有帮助。",
			want:    []string{"美联储加息", "对科创板影响有限"},
		},
		{
			name:    "行首尾空白被去掉、空行被丢弃",
			content: `{"lines": [" 第一行 ", "", "第二行"]}`,
			want:    []string{"第一行", "第二行"},
		},
		{
			name:    "单行结果",
			content: `{"lines": ["只有一行"]}`,
			want:    []string{"只有一行"},
		},
		{
			name:    "空内容报错",
			content: "   ",
			wantErr: true,
		},
		{
			name:    "非 JSON 报错",
			content: "第一行\n第二行",
			wantErr: true,
		},
		{
			name:    "lines 非字符串数组报错",
			content: `{"lines": [1, 2]}`,
			wantErr: true,
		},
		{
			name:    "lines 全空报错",
			content: `{"lines": ["", "  "]}`,
			wantErr: true,
		},
		{
			name:    "对象中无 lines 字段报错",
			content: `{"result": "ok"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCaptionLines(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("lines = %q, want %q", got, tt.want)
			}
		})
	}
}
