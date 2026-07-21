package main

import (
	"context"
	"errors"
	"flag"
	"testing"
)

func TestRunHelpSupportsExplicitAndLegacyForms(t *testing.T) {
	for _, args := range [][]string{{"run", "-help"}, {"-help"}, {"dataset-check", "-help"}} {
		if err := run(context.Background(), args); !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("run(%v) error = %v, want flag.ErrHelp", args, err)
		}
	}
}

func TestRunRejectsUnknownSubcommand(t *testing.T) {
	if err := run(context.Background(), []string{"unknown"}); err == nil {
		t.Fatal("unknown subcommand succeeded")
	}
}
