package llm

import (
	"reflect"
	"testing"
)

func TestParseIndices(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []int
		wantErr bool
	}{
		{
			name:    "plain_array",
			content: `[2, 5, 9, 13]`,
			want:    []int{2, 5, 9, 13},
		},
		{
			name:    "empty_array",
			content: `[]`,
			want:    []int{},
		},
		{
			name:    "markdown_fence",
			content: "```json\n[0, 1, 3]\n```",
			want:    []int{0, 1, 3},
		},
		{
			name:    "with_prefix_text",
			content: "选中结果如下：\n[1, 4]\n完成",
			want:    []int{1, 4},
		},
		{
			name:    "single",
			content: `[0]`,
			want:    []int{0},
		},
		{
			name:    "empty_content",
			content: "",
			wantErr: true,
		},
		{
			name:    "not_array",
			content: `{"ok":true}`,
			wantErr: true,
		},
		{
			name:    "object_with_indices_field_extracts_array",
			content: `{"indices":[1,2]}`,
			want:    []int{1, 2},
		},
		{
			name:    "invalid_json",
			content: `[1, 2,`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIndices(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseIndices() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseIndices() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
