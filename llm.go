package main

import (
	"os"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func NewLLM() (llms.Model, error) {
	baseURL := "http://localhost:8080/v1"
	if host := os.Getenv("KRONK_WEB_API_HOST"); host != "" {
		baseURL = host + "/v1"
	}

	model := "Qwen3.5-9B-Q4_0"
	if value := os.Getenv("LLM_MODEL"); value != "" {
		model = value
	}

	return openai.New(
		openai.WithBaseURL(baseURL),
		openai.WithToken("x"),
		openai.WithModel(model),
	)
}
