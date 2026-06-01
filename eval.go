package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tmc/langchaingo/llms"
	"gopkg.in/yaml.v3"
)

//go:embed prompts/triage.txt
var triagePrompt string

//go:embed prompts/judge.txt
var judgePrompt string

//go:embed testdata/cases.yaml
var casesYAML []byte

type Case struct {
	ID           string   `yaml:"id"`
	Ticket       string   `yaml:"ticket"`
	Gold         Gold     `yaml:"gold"`
	ReplyRubric  string   `yaml:"reply_rubric"`
	ReplyMust    []string `yaml:"reply_must"`
	ReplyMustNot []string `yaml:"reply_must_not"`
}

type Gold struct {
	Category   string `yaml:"category"`
	Severity   string `yaml:"severity"`
	NeedsHuman bool   `yaml:"needs_human"`
	Assignee   string `yaml:"assignee"`
}

type Prediction struct {
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	NeedsHuman bool   `json:"needs_human"`
	Assignee   string `json:"assignee"`
	Reply      string `json:"reply"`
}

type Score struct {
	Parsed       bool   `json:"parsed"`
	CategoryOK   bool   `json:"category_ok"`
	SeverityOK   bool   `json:"severity_ok"`
	NeedsHumanOK bool   `json:"needs_human_ok"`
	AssigneeOK   bool   `json:"assignee_ok"`
	JudgeScore   int    `json:"judge_score"`
	JudgeReason  string `json:"judge_reason"`
	Pass         bool   `json:"pass"`
}

type ToolTrace struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Trace struct {
	CaseID      string      `json:"case_id"`
	Ticket      string      `json:"ticket"`
	RawOutput   string      `json:"raw_output"`
	Prediction  *Prediction `json:"prediction,omitempty"`
	ParseError  string      `json:"parse_error,omitempty"`
	ToolCalls   []ToolTrace `json:"tool_calls"`
	JudgeOutput string      `json:"judge_output,omitempty"`
	Score       Score       `json:"score"`
}

type JudgeResult struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

func RunEval(ctx context.Context, model llms.Model) error {
	cases, err := LoadCases()
	if err != nil {
		return err
	}

	if err := os.MkdirAll("traces", 0o755); err != nil {
		return err
	}

	total, passed, parsed, judgeTotal := len(cases), 0, 0, 0
	failed := make([]string, 0)

	for _, c := range cases {
		trace, err := EvaluateCase(ctx, model, c)
		if err != nil {
			return fmt.Errorf("%s: %w", c.ID, err)
		}
		if err := SaveTrace(trace); err != nil {
			return err
		}

		if trace.Score.Parsed {
			parsed++
		}
		if trace.Score.Pass {
			passed++
		} else {
			failed = append(failed, c.ID)
		}
		judgeTotal += trace.Score.JudgeScore
	}

	fmt.Printf("cases: %d\n", total)
	fmt.Printf("passed: %d\n", passed)
	fmt.Printf("parse ok: %d\n", parsed)
	if total > 0 {
		fmt.Printf("judge average: %.1f\n", float64(judgeTotal)/float64(total))
	}
	if len(failed) > 0 {
		fmt.Printf("failed: %s\n", strings.Join(failed, ", "))
	}

	return nil
}

func EvaluateCase(ctx context.Context, model llms.Model, c Case) (Trace, error) {
	trace := Trace{CaseID: c.ID, Ticket: c.Ticket}
	messages := []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeSystem, triagePrompt),
		llms.TextParts(llms.ChatMessageTypeHuman, c.Ticket),
	}

	for {
		resp, err := model.GenerateContent(ctx, messages, llms.WithTools(AssignmentTools()))
		if err != nil {
			return Trace{}, err
		}
		if len(resp.Choices) == 0 {
			return Trace{}, fmt.Errorf("triage: no choices")
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
		trace.Score = Score{Parsed: false}
		return trace, nil
	}

	trace.Prediction = &pred
	score := ScoreGold(c.Gold, pred)
	judge, judgeOutput, err := JudgeReply(ctx, model, c, pred)
	if err != nil {
		return Trace{}, err
	}

	trace.JudgeOutput = judgeOutput
	score.JudgeScore = judge.Score
	score.JudgeReason = judge.Reason
	score.Pass = score.CategoryOK && score.SeverityOK && score.NeedsHumanOK && score.AssigneeOK && score.JudgeScore >= 4
	trace.Score = score

	return trace, nil
}

func JudgeReply(ctx context.Context, model llms.Model, c Case, pred Prediction) (JudgeResult, string, error) {
	user := fmt.Sprintf(judgePrompt, c.Ticket, c.ReplyRubric, strings.Join(c.ReplyMust, ", "), strings.Join(c.ReplyMustNot, ", "), pred.Reply)
	resp, err := model.GenerateContent(ctx, []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, user)})
	if err != nil {
		return JudgeResult{}, "", err
	}
	if len(resp.Choices) == 0 {
		return JudgeResult{}, "", fmt.Errorf("judge: no choices")
	}

	content := resp.Choices[0].Content
	var result JudgeResult
	if err := DecodeJSON(content, &result); err != nil {
		return JudgeResult{}, content, err
	}
	if result.Score < 1 || result.Score > 5 {
		return JudgeResult{}, content, fmt.Errorf("judge score out of range: %d", result.Score)
	}

	return result, content, nil
}

func LoadCases() ([]Case, error) {
	var cases []Case
	if err := yaml.Unmarshal(casesYAML, &cases); err != nil {
		return nil, err
	}

	return cases, nil
}

func SaveTrace(trace Trace) error {
	path := filepath.Join("traces", trace.CaseID+".json")
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(trace)
}

func PrintTrace(id string) error {
	path := filepath.Join("traces", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fmt.Print(string(data))
	return nil
}
