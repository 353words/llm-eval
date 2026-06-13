package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	llm, err := NewLLM()
	if err != nil {
		return err
	}

	return repl(context.Background(), llm, os.Stdin, os.Stdout)
}

func repl(ctx context.Context, llm llms.Model, r io.Reader, w io.Writer) error {
	fmt.Fprintln(w, "Help desk triage. Enter a ticket and press Ctrl-D to exit.")
	fmt.Fprint(w, ">>> ")

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		ticket := strings.TrimSpace(scanner.Text())
		if ticket == "" {
			fmt.Fprint(w, ">>> ")
			continue
		}

		pred, _, err := Triage(ctx, llm, ticket)
		if err != nil {
			fmt.Fprintf(w, "ERROR: %s\n", err)
			fmt.Fprint(w, ">>> ")
			continue
		}

		data, err := json.MarshalIndent(pred, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(w, string(data))
		fmt.Fprint(w, ">>> ")
	}

	return scanner.Err()
}
