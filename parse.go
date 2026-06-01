package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func ParsePrediction(raw string) (Prediction, error) {
	var pred Prediction
	if err := DecodeJSON(raw, &pred); err != nil {
		return Prediction{}, err
	}

	if err := validateEnum("category", pred.Category, "billing", "bug", "account", "feature"); err != nil {
		return Prediction{}, err
	}
	if err := validateEnum("severity", pred.Severity, "low", "medium", "high"); err != nil {
		return Prediction{}, err
	}
	if err := validateEnum("assignee", pred.Assignee, "Morticia", "Gomez", "Wednesday", "Pugsley"); err != nil {
		return Prediction{}, err
	}
	if strings.TrimSpace(pred.Reply) == "" {
		return Prediction{}, fmt.Errorf("reply is required")
	}

	return pred, nil
}

func DecodeJSON(raw string, dst any) error {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	return json.Unmarshal([]byte(raw), dst)
}

func validateEnum(name, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}

	return fmt.Errorf("invalid %s: %q", name, value)
}
