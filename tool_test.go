package main

import "testing"

import "github.com/tmc/langchaingo/llms"

func TestAssignOwner(t *testing.T) {
	tests := map[string]string{
		"billing": "Morticia",
		"bug":     "Gomez",
		"account": "Wednesday",
		"feature": "Pugsley",
	}

	for topic, want := range tests {
		got, err := AssignOwner(topic)
		if err != nil {
			t.Fatalf("%s: %s", topic, err)
		}
		if got != want {
			t.Fatalf("%s: got %q, want %q", topic, got, want)
		}
	}
}

func TestAssignOwnerUnknown(t *testing.T) {
	if _, err := AssignOwner("sales"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunTool(t *testing.T) {
	result, trace := RunTool(llms.ToolCall{
		ID:   "call-1",
		Type: "function",
		FunctionCall: &llms.FunctionCall{
			Name:      "assign_owner",
			Arguments: `{"topic":"bug"}`,
		},
	})

	if result != `{"assignee":"Gomez"}` {
		t.Fatalf("result: %s", result)
	}
	if trace.Error != "" {
		t.Fatalf("trace error: %s", trace.Error)
	}
}

func TestRunToolBadArguments(t *testing.T) {
	_, trace := RunTool(llms.ToolCall{
		FunctionCall: &llms.FunctionCall{
			Name:      "assign_owner",
			Arguments: `{"topic":`,
		},
	})

	if trace.Error == "" {
		t.Fatal("expected trace error")
	}
}
