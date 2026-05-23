package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/kiry163/easyllm"
)

const defaultQwenBaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"

type config struct {
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
	rt := easyllm.NewEngine(
		client,
		easyllm.WithInstructions(cfg.Instructions),
		easyllm.WithTools(weatherTool()),
	)
	result, err := rt.Run(ctx, easyllm.RunRequest{Input: cfg.Prompt})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, result.OutputText); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	cfg := config{
		Transport: "chat",
		BaseURL:   defaultQwenBaseURL,
		Prompt:    "请查询今天北京的天气。",
	}
	fs := flag.NewFlagSet("easyllm-call", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	envPath := ".env"
	fs.StringVar(&envPath, "env", envPath, "path to .env file")
	fs.StringVar(&cfg.Transport, "transport", "chat", "provider transport: chat or responses")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}

	envValues, err := loadDotEnv(envPath)
	if err != nil {
		return cfg, err
	}
	cfg.APIKey = firstNonEmpty(envValues["QWEN_API_KEY"], envValues["DASHSCOPE_API_KEY"], getenv("QWEN_API_KEY"), getenv("DASHSCOPE_API_KEY"))
	cfg.BaseURL = firstNonEmpty(envValues["QWEN_BASE_URL"], envValues["DASHSCOPE_BASE_URL"], getenv("QWEN_BASE_URL"), getenv("DASHSCOPE_BASE_URL"), cfg.BaseURL)
	cfg.Model = firstNonEmpty(envValues["QWEN_MODEL"], getenv("QWEN_MODEL"))
	cfg.Prompt = firstNonEmpty(envValues["EASYLLM_PROMPT"], getenv("EASYLLM_PROMPT"), cfg.Prompt)
	cfg.Instructions = firstNonEmpty(envValues["EASYLLM_INSTRUCTIONS"], getenv("EASYLLM_INSTRUCTIONS"))
	cfg.Transport = firstNonEmpty(envValues["EASYLLM_TRANSPORT"], getenv("EASYLLM_TRANSPORT"), cfg.Transport)
	cfg.Thinking = boolValue(firstNonEmpty(envValues["QWEN_ENABLE_THINKING"], getenv("QWEN_ENABLE_THINKING")), false)
	cfg.Temperature = floatValue(firstNonEmpty(envValues["QWEN_TEMPERATURE"], getenv("QWEN_TEMPERATURE")))
	cfg.TopP = floatValue(firstNonEmpty(envValues["QWEN_TOP_P"], getenv("QWEN_TOP_P")))
	cfg.MaxTokens = intValue(firstNonEmpty(envValues["QWEN_MAX_TOKENS"], getenv("QWEN_MAX_TOKENS")))

	cfg.Transport = strings.TrimSpace(strings.ToLower(cfg.Transport))
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Instructions = strings.TrimSpace(cfg.Instructions)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
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
		return cfg, errors.New("QWEN_API_KEY or DASHSCOPE_API_KEY is required")
	}
	return cfg, nil
}

func buildClient(cfg config) (easyllm.Client, error) {
	clientConfig := easyllm.Config{
		Provider:  easyllm.ProviderQwen,
		APIKey:    cfg.APIKey,
		BaseURL:   cfg.BaseURL,
		Model:     cfg.Model,
		Transport: cfg.Transport,
		Options: map[string]any{
			"thinking": cfg.Thinking,
		},
	}
	if cfg.Temperature > 0 {
		clientConfig.Temperature = &cfg.Temperature
	}
	if cfg.TopP > 0 {
		clientConfig.TopP = &cfg.TopP
	}
	if cfg.MaxTokens > 0 {
		clientConfig.MaxTokens = &cfg.MaxTokens
	}
	return easyllm.NewClient(clientConfig)
}

type weatherArgs struct {
	City string `tool:"name=city,required,desc=City name"`
}

func weatherTool() easyllm.Tool {
	runTool, err := easyllm.NewTool[weatherArgs](weatherToolRunner{})
	if err != nil {
		panic(err)
	}
	return runTool
}

type weatherToolRunner struct{}

func (weatherToolRunner) Name() string {
	return "get_weather"
}

func (weatherToolRunner) Description() string {
	return "Get today's weather for a city"
}

func (weatherToolRunner) Run(ctx context.Context, call easyllm.ToolCallContext, args weatherArgs) (easyllm.ToolResult, error) {
	city := strings.TrimSpace(args.City)
	if city == "" {
		city = "北京"
	}
	return easyllm.ToolResult{
		Message: city + "今天晴，气温 25C，微风。",
		Data: map[string]any{
			"city":        city,
			"weather":     "晴",
			"temperature": "25C",
			"wind":        "微风",
		},
	}, nil
}

func loadDotEnv(path string) (map[string]string, error) {
	values := map[string]string{}
	path = strings.TrimSpace(path)
	if path == "" {
		return values, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open env file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if key != "" {
			values[key] = value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read env file: %w", err)
	}
	return values, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func boolValue(raw string, fallback bool) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func floatValue(raw string) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return value
}

func intValue(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}
