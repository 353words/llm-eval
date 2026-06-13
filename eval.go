package main

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/tmc/langchaingo/llms"
)

//go:embed prompts/triage.txt
var triagePrompt string

type Prediction struct {
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	NeedsHuman bool   `json:"needs_human"`
	Assignee   string `json:"assignee"`
	Reply      string `json:"reply"`
}

type ToolTrace struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Trace struct {
	Ticket     string      `json:"ticket"`
	RawOutput  string      `json:"raw_output"`
	Prediction *Prediction `json:"prediction,omitempty"`
	ParseError string      `json:"parse_error,omitempty"`
	ToolCalls  []ToolTrace `json:"tool_calls"`
}

func Triage(ctx context.Context, model llms.Model, ticket string) (Prediction, Trace, error) {
	trace := Trace{Ticket: ticket}
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, triagePrompt),
		llms.TextParts(llms.ChatMessageTypeHuman, ticket),
	}

	for {
		resp, err := model.GenerateContent(ctx, messages, llms.WithTools(AssignmentTools()))
		if err != nil {
			return Prediction{}, trace, err
		}
		if len(resp.Choices) == 0 {
			return Prediction{}, trace, fmt.Errorf("triage: no choices")
		}

		choice := resp.Choices[0]
		if len(choice.ToolCalls) == 0 {
			trace.RawOutput = choice.Content
			break
		}

		assistant := llms.TextParts(llms.ChatMessageTypeAI, choice.Content)
		for _, call := range choice.ToolCalls {
			assistant.Parts = append(assistant.Parts, call)
		}
		messages = append(messages, assistant)

		for _, call := range choice.ToolCalls {
			result, toolTrace := RunTool(call)
			trace.ToolCalls = append(trace.ToolCalls, toolTrace)
			messages = append(messages, llms.MessageContent{
				Role: llms.ChatMessageTypeTool,
				Parts: []llms.ContentPart{
					llms.ToolCallResponse{
						ToolCallID: call.ID,
						Name:       call.FunctionCall.Name,
						Content:    result,
					},
				},
			})
		}
	}

	pred, err := ParsePrediction(trace.RawOutput)
	if err != nil {
		trace.ParseError = err.Error()
		return Prediction{}, trace, err
	}

	trace.Prediction = &pred
	return pred, trace, nil
}
