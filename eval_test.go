package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
	"gopkg.in/yaml.v3"
)

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

type JudgeResult struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

type EvalTrace struct {
	CaseID      string      `json:"case_id"`
	Ticket      string      `json:"ticket"`
	RawOutput   string      `json:"raw_output"`
	Prediction  *Prediction `json:"prediction,omitempty"`
	ParseError  string      `json:"parse_error,omitempty"`
	ToolCalls   []ToolTrace `json:"tool_calls"`
	JudgeOutput string      `json:"judge_output,omitempty"`
	Score       Score       `json:"score"`
}

func TestLoadCases(t *testing.T) {
	cases, err := loadCases()
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

func TestEval(t *testing.T) {
	if os.Getenv("RUN_LLM_EVAL") == "" {
		t.Skip("set RUN_LLM_EVAL=1 to run the LLM eval")
	}

	llm, err := NewLLM()
	if err != nil {
		t.Fatal(err)
	}

	cases, err := loadCases()
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("traces", 0o755); err != nil {
		t.Fatal(err)
	}

	passed := 0
	for _, c := range cases {
		trace, err := evaluateCase(context.Background(), llm, c)
		if err != nil {
			t.Fatalf("%s: %s", c.ID, err)
		}
		if err := saveTrace(trace); err != nil {
			t.Fatal(err)
		}
		if trace.Score.Pass {
			passed++
		}
	}

	t.Logf("passed %d/%d", passed, len(cases))
	if passed != len(cases) {
		t.Fatalf("some eval cases failed; inspect traces/")
	}
}

func evaluateCase(ctx context.Context, model llms.Model, c Case) (EvalTrace, error) {
	pred, trace, err := Triage(ctx, model, c.Ticket)
	evalTrace := EvalTrace{
		CaseID:     c.ID,
		Ticket:     trace.Ticket,
		RawOutput:  trace.RawOutput,
		Prediction: trace.Prediction,
		ParseError: trace.ParseError,
		ToolCalls:  trace.ToolCalls,
	}
	if err != nil {
		if trace.ParseError == "" {
			return EvalTrace{}, err
		}

		evalTrace.Score = Score{Parsed: false}
		return evalTrace, nil
	}

	score := ScoreGold(c.Gold, pred)
	judge, judgeOutput, err := judgeReply(ctx, model, c, pred)
	if err != nil {
		return EvalTrace{}, err
	}

	evalTrace.JudgeOutput = judgeOutput
	score.JudgeScore = judge.Score
	score.JudgeReason = judge.Reason
	score.Pass = score.CategoryOK && score.SeverityOK && score.NeedsHumanOK && score.AssigneeOK && score.JudgeScore >= 4
	evalTrace.Score = score

	return evalTrace, nil
}

func judgeReply(ctx context.Context, model llms.Model, c Case, pred Prediction) (JudgeResult, string, error) {
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

func ScoreGold(gold Gold, pred Prediction) Score {
	return Score{
		Parsed:       true,
		CategoryOK:   pred.Category == gold.Category,
		SeverityOK:   pred.Severity == gold.Severity,
		NeedsHumanOK: pred.NeedsHuman == gold.NeedsHuman,
		AssigneeOK:   pred.Assignee == gold.Assignee,
	}
}

func TestScoreGold(t *testing.T) {
	gold := Gold{Category: "billing", Severity: "medium", NeedsHuman: true, Assignee: "Morticia"}
	pred := Prediction{Category: "billing", Severity: "low", NeedsHuman: true, Assignee: "Morticia"}

	score := ScoreGold(gold, pred)
	if !score.CategoryOK || score.SeverityOK || !score.NeedsHumanOK || !score.AssigneeOK {
		t.Fatalf("bad score: %+v", score)
	}
}

func loadCases() ([]Case, error) {
	var cases []Case
	if err := yaml.Unmarshal(casesYAML, &cases); err != nil {
		return nil, err
	}

	return cases, nil
}

func saveTrace(trace EvalTrace) error {
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
