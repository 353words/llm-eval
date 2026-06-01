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

LLM demos are easy to make impressive.
The harder question is whether the workflow keeps working when you change the prompt, model, input data, or surrounding code.

In this post, we'll build a small eval system for a support ticket triage workflow.
The workflow classifies a ticket, calls a tool to assign an owner, returns JSON, and writes a trace for every run.

The eval checks four things:

- Can we parse the model output?
- Does the structured output match the gold answer?
- Did the tool call produce the right assignee?
- Is the customer-facing reply good enough according to an LLM judge?

### Setting Up

If you want to follow along, you'll need to clone the code from the GitHub repo.

Next, start an OpenAI-compatible local model server.
The code defaults to [Kronk](https://github.com/ardanlabs/kronk/) at `http://localhost:8080/v1`.
The default model is `Qwen3.5-9B-Q4_0`.
You can set `KRONK_WEB_API_HOST` if your server is running somewhere else.
You can set `LLM_MODEL` to use a different model.
The code uses `github.com/tmc/langchaingo` to talk to the model, as in the previous posts.

Run the eval:

```sh
$ go run . eval
```

To inspect one trace:

```sh
$ go run . trace billing-refund
```

### Application Overview

The application evaluates a support ticket workflow.
For every ticket, the LLM needs to return JSON in this shape:

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
The `reply` field is free text, so we'll use an LLM judge for that part.
The reply is also allowed to have a little Addams-family style humor, as long as it stays useful and respectful.

### The Assignment Tool

The workflow has one tool called `assign_owner`.
It maps a topic to an internal owner:

```text
billing -> Morticia
bug -> Gomez
account -> Wednesday
feature -> Pugsley
```

The tool is intentionally boring.
You don't want the tool itself to be the hard part of the post.
The interesting question is whether the LLM calls it correctly and uses the result in the final JSON.

### Gold Answers

The eval cases live in `testdata/cases.yaml`.
YAML makes the cases easier to scan and edit than JSON, especially when tickets and rubrics get longer.

```yaml
- id: account-email
  ticket: How do I change the email address on my account?
  gold:
    category: account
    severity: low
    needs_human: false
    assignee: Wednesday
```

Gold answers are useful when the expected answer is objective.
For this project, an exact match is appropriate for category, severity, escalation, and assignee.

### Parsing Output

The workflow asks the model to return JSON.
That doesn't mean it always will.

The parser strips an optional markdown code fence, decodes JSON, and validates the enum fields.
If parsing fails, the eval fails before scoring.

This is worth showing even in a small teaching example.
A response that looks good to a human but can't be parsed by your code is not a good response.

### Traces

Every eval case writes a trace file under `traces/`.
The trace records:

- The input ticket
- Tool calls and tool results
- Raw model output
- Parsed prediction
- Parse errors
- Judge output
- Final score

Traces make eval failures debuggable.
Without traces, you only know the score went down.
With traces, you can see whether the problem was the prompt, the tool call, JSON parsing, or the judge.

### LLM As Judge

The free-text reply is subjective.
Instead of exact matching, the eval sends the ticket, rubric, and model reply to a judge prompt.

The judge returns JSON:

```json
{
  "score": 4,
  "reason": "The reply acknowledges the issue and gives a reasonable next step."
}
```

Use an LLM judge for subjective criteria.
Keep exact checks for everything deterministic.

### Eval Report

At the end, the CLI prints a compact report:

```text
cases: 4
passed: 3
parse ok: 4
judge average: 4.2
failed: bug-login
```

The report tells you whether things improved.
The traces tell you why.

### Conclusion

Start evals with a small set of gold answers.
Record traces for every run.
Treat parsing as part of correctness.
Use deterministic checks where you can and LLM judges where exact matching is a poor fit.
