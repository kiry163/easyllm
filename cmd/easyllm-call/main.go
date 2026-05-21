package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kiry163/easyllm/agent"
	"github.com/kiry163/easyllm/provider"
	"github.com/kiry163/easyllm/provider/openai"
	"github.com/kiry163/easyllm/provider/qwen"
)

type config struct {
	Provider     string
	Transport    string
	Model        string
	Prompt       string
	BaseURL      string
	APIKey       string
	Instructions string
	Thinking     bool
	Temperature  float64
	TopP         float64
	MaxTokens    int
}

func main() {
	if err := run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr *os.File) error {
	cfg, err := parseConfig(args, getenv)
	if err != nil {
		return err
	}
	client, err := buildClient(cfg)
	if err != nil {
		return err
	}
	a := agent.Agent{
		Instructions: cfg.Instructions,
		Client:       client,
	}
	result, err := a.Run(ctx, agent.RunRequest{Input: cfg.Prompt})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, result.OutputText); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("easyllm-call", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.Provider, "provider", "openai", "provider name")
	fs.StringVar(&cfg.Transport, "transport", "chat", "provider transport: chat or responses")
	fs.StringVar(&cfg.Model, "model", "", "model name")
	fs.StringVar(&cfg.Prompt, "prompt", "", "input prompt")
	fs.StringVar(&cfg.BaseURL, "base-url", "https://api.openai.com/v1", "provider base url")
	fs.StringVar(&cfg.Instructions, "instructions", "", "system instructions")
	fs.BoolVar(&cfg.Thinking, "enable-thinking", false, "enable provider thinking mode when supported")
	fs.Float64Var(&cfg.Temperature, "temperature", 0, "provider default temperature")
	fs.Float64Var(&cfg.TopP, "top-p", 0, "provider default top_p")
	fs.IntVar(&cfg.MaxTokens, "max-tokens", 0, "provider default max tokens")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.Provider = strings.TrimSpace(strings.ToLower(cfg.Provider))
	cfg.Transport = strings.TrimSpace(strings.ToLower(cfg.Transport))
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Instructions = strings.TrimSpace(cfg.Instructions)
	switch cfg.Provider {
	case "openai":
		cfg.APIKey = strings.TrimSpace(getenv("OPENAI_API_KEY"))
	case "qwen":
		if cfg.BaseURL == "https://api.openai.com/v1" {
			cfg.BaseURL = qwen.DefaultBaseURL
		}
		cfg.APIKey = strings.TrimSpace(getenv("DASHSCOPE_API_KEY"))
	default:
		return cfg, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
	if cfg.Transport != "chat" && cfg.Transport != "responses" {
		return cfg, fmt.Errorf("unsupported transport %q", cfg.Transport)
	}
	if cfg.Model == "" {
		return cfg, errors.New("model is required")
	}
	if cfg.Prompt == "" {
		return cfg, errors.New("prompt is required")
	}
	if cfg.BaseURL == "" {
		return cfg, errors.New("base URL is required")
	}
	if cfg.APIKey == "" {
		if cfg.Provider == "qwen" {
			return cfg, errors.New("DASHSCOPE_API_KEY is required")
		}
		return cfg, errors.New("OPENAI_API_KEY is required")
	}
	return cfg, nil
}

func buildClient(cfg config) (provider.ModelClient, error) {
	switch cfg.Provider {
	case "openai":
		openaiProvider := openai.NewProvider(
			openai.WithAPIKey(cfg.APIKey),
			openai.WithBaseURL(cfg.BaseURL),
		)
		config := openaiChatModelConfig(cfg)
		if cfg.Transport == "responses" {
			return openaiProvider.ResponsesModel(openai.ResponsesModelConfig(config)), nil
		}
		return openaiProvider.ChatModel(config), nil
	case "qwen":
		qwenProvider := qwen.NewProvider(
			qwen.WithAPIKey(cfg.APIKey),
			qwen.WithBaseURL(cfg.BaseURL),
		)
		config := qwenChatModelConfig(cfg)
		if cfg.Transport == "responses" {
			return qwenProvider.ResponsesModel(qwen.ResponsesModelConfig(config)), nil
		}
		return qwenProvider.ChatModel(config), nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}

func openaiChatModelConfig(cfg config) openai.ChatModelConfig {
	config := openai.ChatModelConfig{
		Model: cfg.Model,
	}
	if cfg.Temperature > 0 {
		config.Temperature = &cfg.Temperature
	}
	if cfg.TopP > 0 {
		config.TopP = &cfg.TopP
	}
	if cfg.MaxTokens > 0 {
		config.MaxTokens = &cfg.MaxTokens
	}
	return config
}

func qwenChatModelConfig(cfg config) qwen.ChatModelConfig {
	config := qwen.ChatModelConfig{
		Model: cfg.Model,
	}
	config.Thinking = &cfg.Thinking
	if cfg.Temperature > 0 {
		config.Temperature = &cfg.Temperature
	}
	if cfg.TopP > 0 {
		config.TopP = &cfg.TopP
	}
	if cfg.MaxTokens > 0 {
		config.MaxTokens = &cfg.MaxTokens
	}
	return config
}
