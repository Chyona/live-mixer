package llm

import (
	"reflect"
	"testing"
)

func TestParseAISliceResult(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    AISliceResult
		wantErr bool
	}{
		{
			name: "object",
			content: `{
				"indices":[2,5,9],
				"title":"利率上行的真相",
				"description":"用数据和逻辑拆解本轮加息对资产价格的传导。",
				"topics":["宏观经济","利率周期","资产配置"]
			}`,
			want: AISliceResult{
				Indices:     []int{2, 5, 9},
				Title:       "利率上行的真相",
				Description: "用数据和逻辑拆解本轮加息对资产价格的传导。",
				Topics:      []string{"宏观经济", "利率周期", "资产配置"},
			},
		},
		{
			name:    "indexes_alias",
			content: `{"indexes":[0,1],"title":"开场钩子","description":"介绍","topics":["财经观点","市场观察"]}`,
			want: AISliceResult{
				Indices:     []int{0, 1},
				Title:       "开场钩子",
				Description: "介绍",
				Topics:      []string{"财经观点", "市场观察"},
			},
		},
		{
			name:    "markdown_fence",
			content: "```json\n{\"indices\":[1],\"title\":\"标题一二\",\"description\":\"简介\",\"topics\":[\"话题甲\",\"话题乙\"]}\n```",
			want: AISliceResult{
				Indices:     []int{1},
				Title:       "标题一二",
				Description: "简介",
				Topics:      []string{"话题甲", "话题乙"},
			},
		},
		{
			name:    "prefix_text",
			content: "结果如下：\n{\"indices\":[0],\"title\":\"短视频标题\",\"description\":\"内容介绍\",\"topics\":[\"话题一\",\"话题二\"]}\n完成",
			want: AISliceResult{
				Indices:     []int{0},
				Title:       "短视频标题",
				Description: "内容介绍",
				Topics:      []string{"话题一", "话题二"},
			},
		},
		{
			name:    "legacy_array",
			content: `[2, 5, 9, 13]`,
			want: AISliceResult{
				Indices: []int{2, 5, 9, 13},
				Topics:  []string{},
			},
		},
		{
			name:    "object_without_indices_falls_back_and_fails",
			content: `{"title":"短视频标题","topics":["话题一","话题二"]}`,
			wantErr: true,
		},
		{
			name:    "invalid_json",
			content: `{indices:`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAISliceResult(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAISliceResult() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAISliceResult() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
