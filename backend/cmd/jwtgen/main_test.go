package main

import (
	"strings"
	"testing"
)

func TestRenderTokenOutput(t *testing.T) {
	got := renderTokenOutput("admin@example.com", 7, "header.payload.signature")
	want := `ADMIN_EMAIL=admin@example.com
ADMIN_USER_ID=7
JWT=header.payload.signature
`
	if got != want {
		t.Fatalf("renderTokenOutput() = %q, want %q", got, want)
	}
}

func TestSensitiveWarnings(t *testing.T) {
	stdout := stdoutSensitiveWarning()
	if !strings.Contains(stdout, "WARNING") || !strings.Contains(stdout, "sensitive") {
		t.Fatalf("stdoutSensitiveWarning() should warn about sensitive output: %q", stdout)
	}

	file := fileSensitiveWarning("/tmp/jwt.env")
	if !strings.Contains(file, "WARNING") || !strings.Contains(file, "sensitive") || !strings.Contains(file, "/tmp/jwt.env") {
		t.Fatalf("fileSensitiveWarning() should name the output file and warn: %q", file)
	}
}
