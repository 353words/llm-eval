## Evaluating LLM Workflows in Go
+++
title = "Evaluating LLM Workflows in Go"
date = "FIXME"
tags = ["golang"]
categories = ["golang", "llm", "ai", "eval"]
url = "FIXME"
author = "mikit"
+++

### Introduction

The main issue with testing agentic workflows is that they are random.
You ask the agent the same question, and the output will be slightly different.
In this post, we'll discuss "eval" - testing your workflow so you'll know they continue to work in production even if you change the prompt, model or the code.

In this post we'll test a small support ticket triage workflow.
The system will classifies a ticket, call a tool to find who to assign to the ticket.

### How Does It Work?

An eval is a test for a non-deterministic system.
You can't assert on the exact bytes the model returns, so you score the output instead.

We'll score four things for every ticket:

- Can we parse the model output?
- Does the structured output match the gold answer?
- Did the tool call produce the right assignee?
- Is the customer-facing reply good enough according to an LLM judge?

The first three are deterministic, so we use exact checks.
The reply is free text, so we hand it to a second LLM acting as a judge.

### Setting Up

If you want to follow along, you'll need to clone the code from [the GitHub repo](https://github.com/353words/llm-eval).

Next, start an OpenAI-compatible local model server.
The code defaults to [kronk](https://github.com/ardanlabs/kronk/) at `http://localhost:8080/v1` with the `Qwen3.5-9B-Q4_0` model, as in the previous posts.
You can set `KRONK_WEB_API_HOST` to point at a different server and `LLM_MODEL` to use a different model.
The code uses `github.com/tmc/langchaingo` to talk to the model.

_Note: You can use [ollama](https://ollama.com/), OpenAI, Claude and many other systems as well. I'm using `kronk` since it runs locally (no charges) and supports the OpenAI API._

Run the help desk REPL:

```sh
$ go run .
Help desk triage. Enter a ticket and press Ctrl-D to exit.
>>> I was charged twice this month and I want a refund.
{
  "category": "billing",
  "severity": "medium",
  "needs_human": true,
  "assignee": "Morticia",
  "reply": "Thanks for reporting the duplicate charge. Morticia will review it carefully and follow up before this bill rises again."
}
```

### Application Overview

The application is a tiny help desk REPL for a support ticket workflow.
For every ticket, the LLM returns a single JSON object:

```json
{
  "category": "billing",
  "severity": "medium",
  "needs_human": true,
  "assignee": "Morticia",
  "reply": "Thanks for reporting the duplicate charge. Morticia will review it carefully and follow up before this bill rises again."
}
```

The `category`, `severity`, `needs_human`, and `assignee` fields are deterministic enough for gold-answer checks.
The `reply` field is free text, so we'll judge it with an LLM.
The reply is also allowed a little Addams-family style humor, as long as it stays useful and respectful.

### The Triage Prompt

The system prompt tells the model how to classify the ticket, when to escalate, and exactly what JSON to return.

**Listing 1: System Prompt**

```text
001 You are a support ticket triage assistant.
002 Your support team has a tasteful Addams-family style: warm, deadpan, and a little macabre.
003 Use one light joke or spooky phrase in the customer-facing reply when it fits.
004 Do not make jokes about refunds, security, outages, angry customers, or blocked access.
005 The reply must still be useful, respectful, and concise.
006 
007 Classify the user's ticket into exactly one category:
008 - billing
009 - bug
010 - account
011 - feature
012 
013 Choose severity as low, medium, or high.
014 Set needs_human to true when the customer is angry, blocked from working, asks for a refund, or the issue is high severity.
015 Use the assign_owner tool with the selected category to find the assignee.
016 
017 Return exactly one JSON object and no markdown:
018 {
019   "category": "billing|bug|account|feature",
020   "severity": "low|medium|high",
021   "needs_human": true,
022   "assignee": "owner returned by the tool",
023   "reply": "short useful customer-facing reply with light Addams-style humor when appropriate"
024 }
```

Listing 1 shows the triage prompt, embedded from `prompts/triage.txt`.
On lines 7-11 you constrain the category to a fixed set, which makes the field checkable later.
On line 15 you instruct the model to use the `assign_owner` tool instead of guessing the owner.
On lines 17-24 you pin down the output shape so your code has a fighting chance of parsing it.

### The Assignment Tool

The workflow has one tool called `assign_owner`.
It maps a topic to an internal owner.

**Listing 2: Tool Definition**

```go
015 func AssignmentTools() []llms.Tool {
016     return []llms.Tool{
017         {
018             Type: "function",
019             Function: &llms.FunctionDefinition{
020                 Name:        "assign_owner",
021                 Description: "Return the internal owner for a support ticket topic.",
022                 Parameters: map[string]any{
023                     "type": "object",
024                     "properties": map[string]any{
025                         "topic": map[string]any{
026                             "type":        "string",
027                             "description": "One of billing, bug, account, or feature.",
028                         },
029                     },
030                     "required": []string{"topic"},
031                 },
032             },
033         },
034     }
035 }
```

Listing 2 shows the tool definition.
On line 20 you name the tool and on lines 22-31 you describe its single `topic` argument.
The tool itself is intentionally boring; you don't want the tool to be the hard part of the post.
The interesting question is whether the LLM calls it correctly and uses the result in the final JSON.

**Listing 3: Running the Tool**

```go
037 func RunTool(call llms.ToolCall) (string, ToolTrace) {
038     trace := ToolTrace{Name: call.FunctionCall.Name, Arguments: call.FunctionCall.Arguments}
039     if call.FunctionCall.Name != "assign_owner" {
040         trace.Error = fmt.Sprintf("unsupported tool: %q", call.FunctionCall.Name)
041         return `{"error":"unsupported tool"}`, trace
042     }
043 
044     var args AssignArgs
045     if err := json.Unmarshal([]byte(call.FunctionCall.Arguments), &args); err != nil {
046         trace.Error = err.Error()
047         return `{"error":"invalid arguments"}`, trace
048     }
049 
050     owner, err := AssignOwner(args.Topic)
051     if err != nil {
052         trace.Error = err.Error()
053         return fmt.Sprintf(`{"error":%q}`, err.Error()), trace
054     }
055 
056     result := fmt.Sprintf(`{"assignee":%q}`, owner)
057     trace.Result = result
058     return result, trace
059 }
060 
061 func AssignOwner(topic string) (string, error) {
062     switch strings.ToLower(strings.TrimSpace(topic)) {
063     case "billing":
064         return "Morticia", nil
065     case "bug":
066         return "Gomez", nil
067     case "account":
068         return "Wednesday", nil
069     case "feature":
070         return "Pugsley", nil
071     default:
072         return "", fmt.Errorf("unknown topic: %q", topic)
073     }
074 }
```

Listing 3 shows `RunTool` and `AssignOwner`.
On lines 44-48 you parse the JSON arguments the model passed and on line 50 you call the business logic.
`RunTool` returns both the JSON to hand back to the model and a `ToolTrace` that records what happened.
Every branch fills in `trace`, so a bad tool call shows up in the trace instead of vanishing.
On lines 61-74 `AssignOwner` is the actual mapping, and it stays a plain function with no LLM in sight.

### Running the Workflow

`Triage` runs the same tool-calling loop as the previous post, but it also builds a trace and parses the result.

**Listing 4: Triage**

```go
037 func Triage(ctx context.Context, model llms.Model, ticket string) (Prediction, Trace, error) {
038     trace := Trace{Ticket: ticket}
039     messages := []llms.MessageContent{
040         llms.TextParts(llms.ChatMessageTypeSystem, triagePrompt),
041         llms.TextParts(llms.ChatMessageTypeHuman, ticket),
042     }
043 
044     for {
045         resp, err := model.GenerateContent(ctx, messages, llms.WithTools(AssignmentTools()))
046         if err != nil {
047             return Prediction{}, trace, err
048         }
049         if len(resp.Choices) == 0 {
050             return Prediction{}, trace, fmt.Errorf("triage: no choices")
051         }
052 
053         choice := resp.Choices[0]
054         if len(choice.ToolCalls) == 0 {
055             trace.RawOutput = choice.Content
056             break
057         }
058 
059         assistant := llms.TextParts(llms.ChatMessageTypeAI, choice.Content)
060         for _, call := range choice.ToolCalls {
061             assistant.Parts = append(assistant.Parts, call)
062         }
063         messages = append(messages, assistant)
064 
065         for _, call := range choice.ToolCalls {
066             result, toolTrace := RunTool(call)
067             trace.ToolCalls = append(trace.ToolCalls, toolTrace)
068             messages = append(messages, llms.MessageContent{
069                 Role: llms.ChatMessageTypeTool,
070                 Parts: []llms.ContentPart{
071                     llms.ToolCallResponse{
072                         ToolCallID: call.ID,
073                         Name:       call.FunctionCall.Name,
074                         Content:    result,
075                     },
076                 },
077             })
078         }
079     }
080 
081     pred, err := ParsePrediction(trace.RawOutput)
082     if err != nil {
083         trace.ParseError = err.Error()
084         return Prediction{}, trace, err
085     }
086 
087     trace.Prediction = &pred
088     return pred, trace, nil
089 }
```

Listing 4 shows `Triage`.
On lines 44-79 you loop, calling the model until it stops asking for tools, exactly as in the meeting scheduler.
The difference is line 67, where every tool call is appended to `trace.ToolCalls`, and line 55, where the final raw output is saved.
On lines 81-85 you parse the raw output into a `Prediction` and record the parse error in the trace if it fails.
`Triage` returns the prediction, the trace, and an error, so the REPL gets a clean answer while the eval gets the full story.

### Parsing Output

The workflow asks the model to return JSON.
That doesn't mean it always will.

**Listing 5: Parsing the Prediction**

```go
009 func ParsePrediction(raw string) (Prediction, error) {
010     var pred Prediction
011     if err := DecodeJSON(raw, &pred); err != nil {
012         return Prediction{}, err
013     }
014 
015     if err := validateEnum("category", pred.Category, "billing", "bug", "account", "feature"); err != nil {
016         return Prediction{}, err
017     }
018     if err := validateEnum("severity", pred.Severity, "low", "medium", "high"); err != nil {
019         return Prediction{}, err
020     }
021     if err := validateEnum("assignee", pred.Assignee, "Morticia", "Gomez", "Wednesday", "Pugsley"); err != nil {
022         return Prediction{}, err
023     }
024     if strings.TrimSpace(pred.Reply) == "" {
025         return Prediction{}, fmt.Errorf("reply is required")
026     }
027 
028     return pred, nil
029 }
030 
031 func DecodeJSON(raw string, dst any) error {
032     raw = strings.TrimSpace(raw)
033     raw = strings.TrimPrefix(raw, "```json")
034     raw = strings.TrimPrefix(raw, "```")
035     raw = strings.TrimSuffix(raw, "```")
036     raw = strings.TrimSpace(raw)
037 
038     return json.Unmarshal([]byte(raw), dst)
039 }
```

Listing 5 shows the parser.
On lines 32-36 `DecodeJSON` strips an optional markdown code fence before decoding, because models love to wrap JSON in ` ```json `.
On lines 15-23 you validate the enum fields, so an answer with `category: "other"` fails here instead of slipping through.
If parsing fails, the eval fails before scoring.
A response that looks good to a human but can't be parsed by your code is not a good response.

### Gold Answers

The eval cases live in `testdata/cases.yaml`.
YAML makes the cases easier to scan and edit than JSON, especially when tickets and rubrics get longer.

**Listing 6: An Eval Case**

```yaml
- id: billing-refund
  ticket: >
    I was charged twice this month and I want a refund.
    This is really frustrating.
  gold:
    category: billing
    severity: medium
    needs_human: true
    assignee: Morticia
  reply_rubric: >
    Acknowledge the duplicate charge, say the billing team will review it,
    and avoid promising an immediate refund. Do not joke about the refund.
```

Listing 6 shows one case.
The `gold` block is the objective answer we expect, and the `reply_rubric` is the instruction we'll later give the judge.
Gold answers are useful when the expected answer is objective; for this project an exact match is appropriate for category, severity, escalation, and assignee.

**Listing 7: Scoring Against Gold**

```go
142 func ScoreGold(gold Gold, pred Prediction) Score {
143     return Score{
144         CategoryOK:   pred.Category == gold.Category,
145         SeverityOK:   pred.Severity == gold.Severity,
146         NeedsHumanOK: pred.NeedsHuman == gold.NeedsHuman,
147         AssigneeOK:   pred.Assignee == gold.Assignee,
148     }
149 }
```

Listing 7 shows `ScoreGold`.
It's just four equality checks, one per deterministic field.
The `AssigneeOK` check on line 147 is the one that tells you whether the tool call actually flowed through into the final answer.

### LLM As Judge

The free-text reply is subjective.
Instead of exact matching, the eval sends the ticket, rubric, and model reply to a judge prompt and asks for a score.

**Listing 8: Judge Prompt**

```text
001 Judge the support reply.
002 
003 Return exactly one JSON object and no markdown:
004 {
005   "score": 1,
006   "reason": "short reason"
007 }
008 
009 Score from 1 to 5:
010 1 means the reply is wrong or unsafe.
011 3 means the reply is acceptable but incomplete.
012 5 means the reply fully satisfies the rubric.
013 
014 Ticket:
015 %s
016 
017 Rubric:
018 %s
019 
020 Reply:
021 %s
```

Listing 8 shows the judge prompt, embedded from `prompts/judge.txt`.
On lines 14-21 the three `%s` placeholders get filled with the ticket, the rubric, and the model's reply.
Asking for a 1-5 score with a reason gives you a number to threshold on and an explanation for the trace.

**Listing 9: Calling the Judge**

```go
161 func judgeReply(ctx context.Context, model llms.Model, c Case, reply string) (JudgeResult, string, error) {
162     prompt := fmt.Sprintf(judgePrompt, c.Ticket, c.ReplyRubric, reply)
163     resp, err := model.GenerateContent(ctx, []llms.MessageContent{llms.TextParts(llms.ChatMessageTypeHuman, prompt)})
164     if err != nil {
165         return JudgeResult{}, "", err
166     }
167 
168     if len(resp.Choices) == 0 {
169         return JudgeResult{}, "", fmt.Errorf("judge: no choices")
170     }
171 
172     content := resp.Choices[0].Content
173     var result JudgeResult
174     if err := DecodeJSON(content, &result); err != nil {
175         return JudgeResult{}, content, err
176     }
177 
178     if result.Score < 1 || result.Score > 5 {
179         return JudgeResult{}, content, fmt.Errorf("judge score out of range: %d", result.Score)
180     }
181 
182     return result, content, nil
183 }
```

Listing 9 shows `judgeReply`.
On line 162 you fill the prompt and on line 163 you make a plain LLM call with no tools.
On line 174 you reuse the same `DecodeJSON` from the parser, because the judge returns JSON too and can also wrap it in a fence.
On lines 178-180 you reject scores outside 1-5, so a confused judge is an error rather than a silent pass.
Use an LLM judge for subjective criteria and keep exact checks for everything deterministic.

### Putting It Together

`evaluateCase` runs the workflow, scores the gold fields, judges the reply, and decides pass or fail.

**Listing 10: evaluateCase**

```go
115 func evaluateCase(ctx context.Context, model llms.Model, c Case) (EvalTrace, error) {
116     pred, trace, err := Triage(ctx, model, c.Ticket)
117     eval := EvalTrace{CaseID: c.ID, Trace: trace}
118     if err != nil {
119         // A parse error is a normal failing case; anything else is a real error.
120         if trace.ParseError == "" {
121             return EvalTrace{}, err
122         }
123 
124         return eval, nil
125     }
126 
127     eval.Score = ScoreGold(c.Gold, pred)
128     judge, output, err := judgeReply(ctx, model, c, pred.Reply)
129     if err != nil {
130         return EvalTrace{}, err
131     }
132 
133     eval.JudgeOutput = output
134     eval.Score.JudgeScore = judge.Score
135     eval.Score.JudgeReason = judge.Reason
136     eval.Score.Pass = eval.Score.CategoryOK && eval.Score.SeverityOK &&
137         eval.Score.NeedsHumanOK && eval.Score.AssigneeOK && judge.Score >= 4
138 
139     return eval, nil
140 }
```

Listing 10 shows `evaluateCase`.
On lines 118-125 you split two kinds of failure: a parse error is a normal failing case that should still be scored and traced, while a transport error is a real error that aborts the run.
On line 127 you score the gold fields and on line 128 you judge the reply.
On lines 136-137 a case passes only when every gold field matches and the judge score is at least 4.

### The Eval Test

The eval runs as a Go test, not as application code.
By default the test is skipped so `go test ./...` doesn't require a running model.

**Listing 11: TestEval**

```go
074 func TestEval(t *testing.T) {
075     if os.Getenv("RUN_LLM_EVAL") == "" {
076         t.Skip("set RUN_LLM_EVAL=1 to run the LLM eval")
077     }
078 
079     llm, err := NewLLM()
080     if err != nil {
081         t.Fatal(err)
082     }
083 
084     cases, err := loadCases()
085     if err != nil {
086         t.Fatal(err)
087     }
088 
089     if err := os.MkdirAll("traces", 0o755); err != nil {
090         t.Fatal(err)
091     }
092 
093     passed := 0
094     for _, c := range cases {
095         trace, err := evaluateCase(context.Background(), llm, c)
096         if err != nil {
097             t.Fatalf("%s: %s", c.ID, err)
098         }
099 
100         if err := saveTrace(trace); err != nil {
101             t.Fatal(err)
102         }
103 
104         if trace.Score.Pass {
105             passed++
106         }
107     }
108 
109     t.Logf("passed %d/%d", passed, len(cases))
110     if passed != len(cases) {
111         t.Fatalf("some eval cases failed; inspect traces/")
112     }
113 }
```

Listing 11 shows `TestEval`.
On lines 75-77 the test skips itself unless `RUN_LLM_EVAL=1` is set.
On lines 94-107 you evaluate every case, save its trace, and count the passes.
On lines 109-112 you print a compact report and fail the test if any case failed.

Run it with:

```sh
$ RUN_LLM_EVAL=1 go test -run TestEval -v
```

At the end the test prints:

```text
eval_test.go:109: passed 3/4
```

The report tells you whether things improved.
The traces tell you why.

### Traces

Every case writes a trace file under `traces/` recording the ticket, tool calls and results, raw model output, parsed prediction, parse errors, judge output, and the final score.

Traces make eval failures debuggable.
Without traces, you only know the score went down.
With traces, you can see whether the problem was the prompt, the tool call, JSON parsing, or the judge.

### Summary

In a few hundred lines we wrote an eval for an LLM workflow.
Start evals with a small set of gold answers.
Record traces for every run.
Treat parsing as part of correctness.
Use deterministic checks where you can and LLM judges where exact matching is a poor fit.

As usual, you can see the code [in the GitHub repo](https://github.com/353words/llm-eval).

How do you evaluate your LLM workflows? Let me know at miki@ardanlabs.com
