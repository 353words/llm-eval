## Using Tools: A Meeting Scheduler
++
title = "Using Tools: A Meeting Scheduler"
date = "FIXME"
tags = ["golang"]
categories = ["golang", "llm", "ai", "tool", "function"]
url = "FIXME"
author = "mikit"
+++

### Introduction

LLM are great, but they are trained on public data sets.
In some cases, you need the LLM to use data that's not publicly available or that's frequently changing.
There are several ways to make such data available to LLMs:

- Tool/function calls
- [Retrieval-augmented generation](https://en.wikipedia.org/wiki/Retrieval-augmented_generation) (aka RAG)
- [MCP](https://en.wikipedia.org/wiki/Model_Context_Protocol)

In coding agents, you can also add skills.

In this post we'll focus on function calling.

### How Does It Work?

When asking the LLM, you also provide a description of available tools.
If the LLM need more information, it'll return a reply that contains a tool call.
You need to call your function and return the answer to the LLM.

This means that a single query can result in several round trips between your agent and the LLM until you get the final answer.

### Setting Up

If you want to follow along, you'll need to clone the code from [the GitHub repo](https://github.com/353words/llm-tool).

Next, you need to install our very own [kronk](https://github.com/ardanlabs/kronk/) as the system to run LLMs.
kronk works as a server, but can also be used directly from your code. We'll use the format approach.

_Note: You can use [ollama](https://ollama.com/), OpenAI, Claude and many other systems as well. I'm using `kronk` since it runs locally (no charges) and supports OpenAI API._

Run `go install github.com/ardanlabs/kronk/cmd/kronk@latest`, `kronk` will be installed to `$(go env GOPATH)/bin`, 
which in most systems is `~/go/bin`. You can run `kronk` as `~/go/bin/kronk` or add `$(go env GOPATH)/bin` to the `PATH` environment variable.

Start kronk with: `kronk server start`.

Next, you need to install the model, we're going to use the `ministral` model. In a second terminal run:

```
$ kronk model pull https://huggingface.co/unsloth/Ministral-3-3B-Instruct-2512-GGUF/resolve/main/Ministral-3-3B-Instruct-2512-Q6_K.gguf
```

You might want to use other models, you can query HuggingFace to find a suitable model and then get the URL for the `.gguf` file.


### Application Overview

We're going to create an agent that suggests meeting times between two people.
The agent uses the `QueryMeetings` function that returns what current meetings a user has at a given date.

_Note: You can see the implementation of `QueryMeetings` in [`db.go`](https://github.com/353words/llm-tool/blob/main/db.go)._


### System Prompt

The system prompt instructs the LLM how to answer and that it should use our tool.

**Listing 1: System Prompt**

```go
018 var systemPrompt = `
019 You're a helpful scheduling assistant.
020 
021 When asked to schedule a meeting between multiple people:
022 1. Use the "meetings" tool to get the existing meetings for EACH participant separately
023 2. Identify ALL blocked time spans from ALL participants
024 3. Propose time slots that work for EVERYONE - the proposed times must NOT overlap with ANY participant's existing meetings
025 4. Ensure the proposed time slots have enough duration for the requested meeting length
026 
027 Return exactly and only the proposed time ranges as HH:MM-HH:MM, one per line, with no extra text.
028 `
```

Listing 1 shows the system prompt, it instructs the LLM to work with the `meetings` tool and puts guardrails around the answer.


### Tool Definition

**Listing 2: Tool Definition**

```go
030 var Tools = []llms.Tool{
031     {
032         Type: "function",
033         Function: &llms.FunctionDefinition{
034             Name:        "meetings",
035             Description: "Get the meetings (busy time) of a user for a given date. Returns a list of meetings.",
036             Parameters: map[string]any{
037                 "type": "object",
038                 "properties": map[string]any{
039                     "user": map[string]any{
040                         "type":        "string",
041                         "description": "User name",
042                     },
043                     "date": map[string]any{
044                         "type":        "string",
045                         "description": "date in YYYY-MM-DD format",
046                     },
047                 },
048                 "required": []string{"user", "date"},
049             },
050         },
051     },
052 }
```

Listing 2 shows the tool definition.
On line 30 you create a slice of `llm.Tool`.
On lines 32-51 you specify the `meetings` tool.
On lines 36 you define the parameters. The `properties` key on line 38 defines the tool arguments.
On line 48 you specify that both `user` and `date` are required.

There's no way to define a schema for the tool output.

### Tool Calling

When an LLM calls a tool, it passes the arguments as a JSON encoded string.
You'll need to parse the arguments and then convert the result to JSON string to return to the LLM.

**Listing 3: callQueryMeetings**

```go
054 func callQueryMeetings(arguments string) (string, error) {
055     var args struct {
056         User string
057         Date string
058     }
059     if err := json.Unmarshal([]byte(arguments), &args); err != nil {
060         return "", err
061     }
062 
063     user := strings.ToLower(args.User)
064     date, err := time.Parse("2006-01-02", args.Date)
065     if err != nil {
066         return "", err
067     }
068 
069     meetings := QueryMeetings(user, date)
070 
071     slog.Debug("meetings", "data", meetings)
072 
073     data, err := json.Marshal(meetings)
074     if err != nil {
075         return "", err
076     }
077 
078     return string(data), nil
079 }
```

Listing 3 shows `callQueryMeetings` that is a wrapper around `QueryMeetings`.
On lines 55-67 you parse the arguments from the LLM.
The date is passed as a string in `YYYY-MM-DD` format, so you need to convert it to time.Time on line 64. 
On line 69 you call `QueryMeetings` and on line 73 you convert the result (`[]Meeting`) to JSON.

### Finding a Meeting

Once you have the building blocks, you can call the LLM.


**Listing 4: findMeetings**

```go
081 func findMeetings(ctx context.Context, llm llms.Model, userPrompt string) (string, error) {
082     messages := []llms.MessageContent{
083         llms.TextParts(llms.ChatMessageTypeSystem, systemPrompt),
084         llms.TextParts(llms.ChatMessageTypeHuman, userPrompt),
085     }
086 
087     // Loop until no more function calls
088     for {
089         resp, err := llm.GenerateContent(ctx, messages, llms.WithTools(Tools))
090         if err != nil {
091             return "", err
092         }
093 
094         respchoice := resp.Choices[0]
095         if len(respchoice.ToolCalls) == 0 {
096             return respchoice.Content, nil
097         }
098 
099         assistantResponse := llms.TextParts(llms.ChatMessageTypeAI, respchoice.Content)
100         for _, tc := range respchoice.ToolCalls {
101             assistantResponse.Parts = append(assistantResponse.Parts, tc)
102         }
103         messages = append(messages, assistantResponse)
104 
105         for _, toolCall := range respchoice.ToolCalls {
106             switch toolCall.FunctionCall.Name {
107             case "meetings":
108                 slog.Debug("tool call", "data", toolCall.FunctionCall.Arguments)
109                 data, err := callQueryMeetings(toolCall.FunctionCall.Arguments)
110                 if err != nil {
111                     return "", err
112                 }
113 
114                 response := llms.MessageContent{
115                     Role: llms.ChatMessageTypeTool,
116                     Parts: []llms.ContentPart{
117                         llms.ToolCallResponse{
118                             ToolCallID: toolCall.ID,
119                             Name:       toolCall.FunctionCall.Name,
120                             Content:    data,
121                         },
122                     },
123                 }
124                 messages = append(messages, response)
125             default:
126                 return "", fmt.Errorf("unsupported tool: %q", toolCall.FunctionCall.Name)
127             }
128         }
129     }
130 }
```

Listing 4 shows `findMeetings`.
On line 82-85 you create the initial call to the LLM.
On line 88 you start a for loop for communicating with the LLM, it'll end when there are no more tool calls.
On lines 89-103 you call the LLM and append the result to the current message history.
On line 95 you check if there are no more tool calls and return the final answer on line 86.
On lines 105-129 you go over tool calls and if there's a call to `meetings` you call it and add the result to the message history.
On lines 118-119 you add the ToolCallID and Name so the LLM will be able to connect the response to the tool call.

### Main

Finally, you're ready to use the agent to schedule a meeting.

Our meeting database has the following entries:

```
miki,2026-06-07,08:30,09:30
miki,2026-06-07,13:30,14:15
bill,2026-06-07,09:00,09:45
bill,2026-06-07,13:00,14:00
```

**Listing 5: main**

```go
132 func main() {
133     baseURL := "http://localhost:8080/v1"
134     if host := os.Getenv("KRONK_WEB_API_HOST"); host != "" {
135         baseURL = host + "/v1"
136     }
137 
138     llm, err := openai.New(
139         openai.WithBaseURL(baseURL),
140         openai.WithToken("x"),
141         openai.WithModel("Ministral-3-3B-Instruct-2512-Q6_K"),
142     )
143     
144     if err != nil {
145         fmt.Fprintf(os.Stderr, "error: %v\n", err)
146         os.Exit(1)
147     }
148 
149     if os.Getenv("DEBUG") != "" {
150         h := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug})
151         log := slog.New(h)
152         slog.SetDefault(log)
153     }
154 
155     query := "Suggest time slots for a 45 minute meeting between Miki & Bill on June 7, 2026."
156     answer, err := findMeetings(context.Background(), llm, query)
157     if err != nil {
158         fmt.Fprintf(os.Stderr, "error: %v\n", err)
159         os.Exit(1)
160     }
161 
162     fmt.Println(answer)
163 }
```

Listing 5 shows `main`.
On lines 133-142 you connect to kronk using the langchain `openai` API that kronk supports.
Since `langchain/openai` requires a token, you pass a dummy one on line 140.
On lines 149-152 you check for `DEBUG` flag, it will show `slog.Debug` logs which help during debugging to view intermediate results.
One lines 156-162 you call `findMeetings` and print out the results.

Let's give it a try:

```
$ go run .
Here are the proposed time slots for a 45-minute meeting between Miki and Bill:

- **10:00 AM - 10:45 AM**
- **14:15 PM - 15:00 PM**
```

### Summary

In about 160 lines of code we wrote a helpful scheduling assistant.
In my experience, some of the models return wrong results that overlap with existing meetings.
It's a good idea to validate the LLM results and filter out overlapping meetings before returning an answer to the user.

As usual, you can see the code and database [in the GitHub repo](https://github.com/353words/llm-tool)

What kind of tools do you use with your LLMs? Let me know at miki@ardanlabs.com
