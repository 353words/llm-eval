package main

import (
	"context"
	"fmt"
	"os"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		usage()
		return nil
	}

	switch os.Args[1] {
	case "eval":
		llm, err := NewLLM()
		if err != nil {
			return err
		}

		return RunEval(context.Background(), llm)
	case "trace":
		if len(os.Args) != 3 {
			return fmt.Errorf("usage: go run . trace <case-id>")
		}

		return PrintTrace(os.Args[2])
	default:
		usage()
		return nil
	}
}

func usage() {
	fmt.Println("usage:")
	fmt.Println("  go run . eval")
	fmt.Println("  go run . trace <case-id>")
}
