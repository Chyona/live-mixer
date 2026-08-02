package service

import (
	"reflect"
	"testing"
)

func TestParseKeywordExpr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want [][]string
	}{
		{name: "empty", raw: "   ", want: nil},
		{name: "and only", raw: " 游戏, 周末 ,, ", want: [][]string{{"游戏", "周末"}}},
		{name: "or only", raw: "游戏|周末", want: [][]string{{"游戏"}, {"周末"}}},
		{name: "and or", raw: "游戏,周末|发布会,2026", want: [][]string{{"游戏", "周末"}, {"发布会", "2026"}}},
		{name: "trim pipes", raw: "|游戏,周末||", want: [][]string{{"游戏", "周末"}}},
		{name: "lower", raw: "Launch,Spring", want: [][]string{{"launch", "spring"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseKeywordExpr(tt.raw)
			if !reflect.DeepEqual([][]string(got), tt.want) {
				t.Fatalf("parseKeywordExpr(%q) = %#v, want %#v", tt.raw, got, tt.want)
			}
		})
	}
}
