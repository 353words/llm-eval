package main

func ScoreGold(gold Gold, pred Prediction) Score {
	score := Score{
		Parsed:       true,
		CategoryOK:   pred.Category == gold.Category,
		SeverityOK:   pred.Severity == gold.Severity,
		NeedsHumanOK: pred.NeedsHuman == gold.NeedsHuman,
		AssigneeOK:   pred.Assignee == gold.Assignee,
	}

	return score
}
