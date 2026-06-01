package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

type AssignArgs struct {
	Topic string `json:"topic"`
}

func AssignmentTools() []llms.Tool {
	return []llms.Tool{
		{
			Type: "function",
			Function: &llms.FunctionDefinition{
				Name:        "assign_owner",
				Description: "Return the internal owner for a support ticket topic.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"topic": map[string]any{
							"type":        "string",
							"description": "One of billing, bug, account, or feature.",
						},
					},
					"required": []string{"topic"},
				},
			},
		},
	}
}

func RunTool(call llms.ToolCall) (string, ToolTrace) {
	trace := ToolTrace{Name: call.FunctionCall.Name, Arguments: call.FunctionCall.Arguments}
	if call.FunctionCall.Name != "assign_owner" {
		trace.Error = fmt.Sprintf("unsupported tool: %q", call.FunctionCall.Name)
		return `{"error":"unsupported tool"}`, trace
	}

	var args AssignArgs
	if err := json.Unmarshal([]byte(call.FunctionCall.Arguments), &args); err != nil {
		trace.Error = err.Error()
		return `{"error":"invalid arguments"}`, trace
	}

	owner, err := AssignOwner(args.Topic)
	if err != nil {
		trace.Error = err.Error()
		return fmt.Sprintf(`{"error":%q}`, err.Error()), trace
	}

	result := fmt.Sprintf(`{"assignee":%q}`, owner)
	trace.Result = result
	return result, trace
}

func AssignOwner(topic string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "billing":
		return "Morticia", nil
	case "bug":
		return "Gomez", nil
	case "account":
		return "Wednesday", nil
	case "feature":
		return "Pugsley", nil
	default:
		return "", fmt.Errorf("unknown topic: %q", topic)
	}
}
