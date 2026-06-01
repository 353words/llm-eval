package main

import "testing"

func TestParsePrediction(t *testing.T) {
	raw := `{"category":"billing","severity":"medium","needs_human":true,"assignee":"Morticia","reply":"We'll review the duplicate charge."}`

	pred, err := ParsePrediction(raw)
	if err != nil {
		t.Fatalf("parse: %s", err)
	}
	if pred.Category != "billing" {
		t.Fatalf("category: %q", pred.Category)
	}
}

func TestParsePredictionCodeFence(t *testing.T) {
	raw := "```json\n{\"category\":\"bug\",\"severity\":\"high\",\"needs_human\":true,\"assignee\":\"Gomez\",\"reply\":\"We are escalating the sign in issue.\"}\n```"

	if _, err := ParsePrediction(raw); err != nil {
		t.Fatalf("parse fenced JSON: %s", err)
	}
}

func TestParsePredictionInvalidJSON(t *testing.T) {
	if _, err := ParsePrediction(`{"category":`); err == nil {
		t.Fatal("expected error")
	}
}

func TestParsePredictionInvalidEnum(t *testing.T) {
	raw := `{"category":"other","severity":"medium","needs_human":true,"assignee":"Morticia","reply":"ok"}`

	if _, err := ParsePrediction(raw); err == nil {
		t.Fatal("expected error")
	}
}
