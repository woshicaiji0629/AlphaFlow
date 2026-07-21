package main

import (
	"context"
	"strings"
	"testing"
)

func TestRunRequiresKnownSubcommand(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}} {
		_, err := run(context.Background(), args)
		if err == nil {
			t.Fatalf("run(%v) succeeded, want error", args)
		}
	}
}

func TestSubcommandHelpDoesNotRunResearch(t *testing.T) {
	for _, command := range []string{"swing", "analysis"} {
		_, err := run(context.Background(), []string{command, "-help"})
		if err == nil || !strings.Contains(err.Error(), "flag: help requested") {
			t.Fatalf("run(%s -help) error = %v", command, err)
		}
	}
}
