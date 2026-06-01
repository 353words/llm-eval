package main

import "testing"

func TestScoreGold(t *testing.T) {
	gold := Gold{Category: "billing", Severity: "medium", NeedsHuman: true, Assignee: "Morticia"}
	pred := Prediction{Category: "billing", Severity: "low", NeedsHuman: true, Assignee: "Morticia"}

	score := ScoreGold(gold, pred)
	if !score.CategoryOK || score.SeverityOK || !score.NeedsHumanOK || !score.AssigneeOK {
		t.Fatalf("bad score: %+v", score)
	}
}
