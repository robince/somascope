package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHandleVersionCommand(t *testing.T) {
	t.Run("prints version", func(t *testing.T) {
		prevVersion := version
		prevCommit := commit
		prevBuildDate := buildDate
		version = "v0.1.0"
		commit = "abc12345"
		buildDate = "2026-03-30T12:00:00Z"
		t.Cleanup(func() {
			version = prevVersion
			commit = prevCommit
			buildDate = prevBuildDate
		})

		var out bytes.Buffer
		if !handleVersionCommand([]string{"--version"}, &out) {
			t.Fatalf("expected version command to be handled")
		}

		got := out.String()
		if !strings.Contains(got, "somascope v0.1.0 (abc12345) 2026-03-30T12:00:00Z") {
			t.Fatalf("unexpected version output: %q", got)
		}
	})

	t.Run("ignores normal startup", func(t *testing.T) {
		var out bytes.Buffer
		if handleVersionCommand([]string{"serve"}, &out) {
			t.Fatalf("expected non-version command to fall through")
		}
		if out.Len() != 0 {
			t.Fatalf("expected no output, got %q", out.String())
		}
	})
}
