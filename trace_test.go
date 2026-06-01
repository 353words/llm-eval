package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveTrace(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("traces", 0o755); err != nil {
		t.Fatalf("mkdir: %s", err)
	}

	trace := Trace{
		CaseID: "case-1",
		Ticket: "ticket",
		Score:  Score{Parsed: true},
	}
	if err := SaveTrace(trace); err != nil {
		t.Fatalf("save trace: %s", err)
	}

	data, err := os.ReadFile(filepath.Join("traces", "case-1.json"))
	if err != nil {
		t.Fatalf("read trace: %s", err)
	}
	if len(data) == 0 {
		t.Fatal("empty trace")
	}
}
