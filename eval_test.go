package main

import "testing"

func TestLoadCases(t *testing.T) {
	cases, err := LoadCases()
	if err != nil {
		t.Fatalf("load cases: %s", err)
	}
	if len(cases) != 4 {
		t.Fatalf("case count: %d", len(cases))
	}
	if cases[0].ID != "billing-refund" {
		t.Fatalf("first case: %q", cases[0].ID)
	}
}
