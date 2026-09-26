package llm

import (
	"reflect"
	"testing"
)

func TestParseCaptionCuts(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []int
		wantErr bool
	}{
		{
			name:    "标准对象",
			content: `{"cuts": [8, 17]}`,
			want:    []int{8, 17},
		},
		{
			name:    "positions 字段名",
			content: `{"positions": [3]}`,
			want:    []int{3},
		},
		{
			name:    "indices 字段名",
			content: `{"indices": [3, 4]}`,
			want:    []int{3, 4},
		},
		{
			name:    "裸数组",
			content: `[8, 17]`,
			want:    []int{8, 17},
		},
		{
			name:    "单个整数",
			content: `8`,
			want:    []int{8},
		},
		{
			name:    "对象里的单个整数",
			content: `{"cuts": 8}`,
			want:    []int{8},
		},
		{
			name:    "markdown 代码块包裹",
			content: "```json\n{\"cuts\": [8]}\n```",
			want:    []int{8},
		},
		{
			name:    "夹杂前后说明文字",
			content: "好的，断句结果如下：\n{\"cuts\": [8]}\n希望有帮助。",
			want:    []int{8},
		},
		{
			name:    "位置写成浮点整数",
			content: `{"cuts": [8.0, 17.0]}`,
			want:    []int{8, 17},
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
			name:    "空位置数组报错",
			content: `{"cuts": []}`,
			wantErr: true,
		},
		{
			name:    "无 cuts 字段报错",
			content: `{"result": "ok"}`,
			wantErr: true,
		},
		{
			name:    "位置不是整数报错",
			content: `{"cuts": [8.5]}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCaptionCuts(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("期望报错，实际得到 %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("cuts = %v, want %v", got, tt.want)
			}
		})
	}
}
