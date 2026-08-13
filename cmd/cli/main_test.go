package main

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewBuilderRejectsNilOptions(t *testing.T) {
	t.Parallel()

	_, err := newBuilder(&cobra.Command{}, nil)
	if err == nil || !strings.Contains(err.Error(), "global options are required") {
		t.Fatalf("newBuilder() error = %v, want missing options error", err)
	}
}
