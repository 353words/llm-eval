package main

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func NewLLM() (llms.Model, error) {
	/*
		baseURL := "http://localhost:8080/v1"
		if host := os.Getenv("KRONK_WEB_API_HOST"); host != "" {
			baseURL = host + "/v1"
		}

		//model := "Qwen3.5-9B-Q4_0"
		model := "rnj-1-instruct-Q6_K"
		if value := os.Getenv("LLM_MODEL"); value != "" {
			model = value
		}

			return openai.New(
				openai.WithBaseURL(baseURL),
				openai.WithToken("x"),
				openai.WithModel(model),
			)
	*/
	return openai.New(openai.WithToken(os.Getenv("OPENAI_API_KEY")))
}

//go:embed prompts/triage.txt
var triagePrompt string

type Prediction struct {
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	NeedsHuman bool   `json:"needs_human"`
	Assignee   string `json:"assignee"`
	Reply      string `json:"reply"`

	trace Trace
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

func ParsePrediction(raw string) (Prediction, error) {
	var pred Prediction
	fmt.Println("[DEBUG]", raw)
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

	// Decode only the first JSON value; LLMs sometimes emit a trailing
	// duplicate object, which json.Unmarshal rejects as trailing data.
	return json.NewDecoder(strings.NewReader(raw)).Decode(dst)
}

func validateEnum(name, value string, allowed ...string) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}

	return fmt.Errorf("invalid %s: %q", name, value)
}

func Triage(ctx context.Context, model llms.Model, ticket string) (Prediction, error) {
	trace := Trace{Ticket: ticket}
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, triagePrompt),
		llms.TextParts(llms.ChatMessageTypeHuman, ticket),
	}

	for {
		resp, err := model.GenerateContent(ctx, messages, llms.WithTools(AssignmentTools()))
		if err != nil {
			return Prediction{trace: trace}, err
		}
		if len(resp.Choices) == 0 {
			return Prediction{trace: trace}, fmt.Errorf("triage: no choices")
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
		return Prediction{trace: trace}, err
	}

	trace.Prediction = &pred
	pred.trace = trace
	return pred, nil
}

func main() {
	llm, err := NewLLM()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: create LLM - %s\n", err)
		os.Exit(1)
	}

	const prefix = ">>> "
	fmt.Println("Welcome to help desk, how can we hurt you today? (CTRL-D to quit)")
	fmt.Print(prefix)

	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		ticket := strings.TrimSpace(s.Text())
		if ticket == "" {
			fmt.Print(prefix)
			continue
		}

		pred, err := Triage(context.Background(), llm, ticket)
		if err != nil {
			fmt.Printf("ERROR: %s\n", err)
			fmt.Print(prefix)
			continue
		}

		fmt.Printf("%#v\n", pred)
		fmt.Print(prefix)
	}
}
