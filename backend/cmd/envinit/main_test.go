package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfirmReinit_ExactYES(t *testing.T) {
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetIn(strings.NewReader("YES\n"))

	ok, err := confirmReinit(cmd)
	if err != nil {
		t.Fatalf("confirmReinit() error = %v", err)
	}
	if !ok {
		t.Fatal("confirmReinit() = false, want true for YES")
	}
}

func TestConfirmReinit_Cancel(t *testing.T) {
	cases := []string{"n\n", "no\n", "\n", "YES!\n", "y\n", "yes\n", "Yes\n", "YEs\n"}
	for _, input := range cases {
		cmd := &cobra.Command{}
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetIn(strings.NewReader(input))
		ok, err := confirmReinit(cmd)
		if err != nil {
			t.Fatalf("input %q: error = %v", input, err)
		}
		if ok {
			t.Fatalf("input %q: got true, want false", input)
		}
	}
}
