package easyllm

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kiry163/easyllm/internal/model"
)

type Engine struct {
	client                Client
	instructions          string
	tools                 []Tool
	hooks                 Hooks
	maxModelCalls         int
	parallelToolExecution bool
	maxToolConcurrency    int
	stopOnToolSuccess     bool
	stopToolName          string
	stopToolReminder      *int
	toolChoice            *ToolChoice
	currentTime           *currentTimeRuntime
}

// CurrentTimeConfig controls the time context injected by an Engine.
type CurrentTimeConfig struct {
	// Location controls the injected time zone. A nil location uses UTC.
	Location *time.Location
}

type currentTimeRuntime struct {
	location *time.Location
	now      func() time.Time
}

type RunRequest struct {
	Session    *Session
	Input      string
	InputParts []model.ContentPart
	Metadata   map[string]any
	// CurrentTimeLocation overrides the engine's configured location for this run.
	CurrentTimeLocation *time.Location
}

type EngineOption func(*Engine)

func WithInstructions(text string) EngineOption {
	return func(e *Engine) {
		e.instructions = text
	}
}

func WithTools(tools ...Tool) EngineOption {
	return func(e *Engine) {
		e.tools = append([]Tool(nil), tools...)
	}
}

func AutoToolChoice() ToolChoice {
	return model.NewToolChoice(model.ToolChoiceModeAuto, "")
}

func RequiredToolChoice() ToolChoice {
	return model.NewToolChoice(model.ToolChoiceModeRequired, "")
}

func NoToolChoice() ToolChoice {
	return model.NewToolChoice(model.ToolChoiceModeNone, "")
}

func NamedToolChoice(name string) ToolChoice {
	return model.NewToolChoice(model.ToolChoiceModeNamed, strings.TrimSpace(name))
}

// WithToolChoice controls tool selection for the first model request of each
// Run or RunStream. Requests after tool execution use automatic selection.
func WithToolChoice(choice ToolChoice) EngineOption {
	return func(e *Engine) {
		choiceCopy := choice
		e.toolChoice = &choiceCopy
	}
}

func WithHooks(hooks Hooks) EngineOption {
	return func(e *Engine) {
		e.hooks = hooks
	}
}

func WithMaxModelCalls(n int) EngineOption {
	return func(e *Engine) {
		if n > 0 {
			e.maxModelCalls = n
		}
	}
}

// WithParallelToolExecution controls whether tool calls from one model response
// may execute concurrently. Parallel execution is enabled by default.
func WithParallelToolExecution(enabled bool) EngineOption {
	return func(e *Engine) {
		e.parallelToolExecution = enabled
	}
}

// WithMaxToolConcurrency limits concurrent tool executions within one Run.
// Zero means no limit beyond the number of calls in the current tool batch.
func WithMaxToolConcurrency(n int) EngineOption {
	return func(e *Engine) {
		e.maxToolConcurrency = n
	}
}

// WithStopOnToolSuccess stops before the next model request when tool succeeds.
// All tool calls in the current model-response batch are completed first.
func WithStopOnToolSuccess(tool Tool) EngineOption {
	return func(e *Engine) {
		e.stopOnToolSuccess = true
		if tool == nil {
			e.stopToolName = ""
			return
		}
		e.stopToolName = tool.Definition().Name
	}
}

// WithStopOnToolSuccessName is the name-based form of WithStopOnToolSuccess.
func WithStopOnToolSuccessName(name string) EngineOption {
	return func(e *Engine) {
		e.stopOnToolSuccess = true
		e.stopToolName = name
	}
}

// WithStopToolReminder configures when the Engine starts reminding the model
// to call the configured stop tool. Zero disables reminders. When omitted,
// reminders start at half of the model-call limit, rounded up.
func WithStopToolReminder(remainingModelCalls int) EngineOption {
	return func(e *Engine) {
		value := remainingModelCalls
		e.stopToolReminder = &value
	}
}

// WithCurrentTime enables current-time injection for Run and RunStream calls.
func WithCurrentTime(config CurrentTimeConfig) EngineOption {
	return func(e *Engine) {
		location := config.Location
		if location == nil {
			location = time.UTC
		}
		e.currentTime = &currentTimeRuntime{
			location: location,
			now:      time.Now,
		}
	}
}

func NewEngine(client Client, opts ...EngineOption) *Engine {
	engine := &Engine{
		client:                client,
		maxModelCalls:         3,
		parallelToolExecution: true,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(engine)
		}
	}
	return engine
}

func (e *Engine) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if err := validateRunRequest(req); err != nil {
		return nil, err
	}
	session := req.Session
	if session == nil {
		session = NewSession()
	}
	if e.instructions != "" && len(session.Items) == 0 {
		session.AppendItems(model.SystemMessageItem{
			Content: []model.TextPart{{Text: e.instructions}},
		})
	}
	if req.Input != "" {
		session.AppendUserText(req.Input)
	}
	if len(req.InputParts) > 0 {
		session.AppendUserContent(req.InputParts...)
	}
	return run(ctx, e.client, session, req.Metadata, runConfig{
		tools:                 e.tools,
		hooks:                 e.hooks,
		maxModelCalls:         e.maxModelCalls,
		parallelToolExecution: e.parallelToolExecution,
		maxToolConcurrency:    e.maxToolConcurrency,
		stopOnToolSuccess:     e.stopOnToolSuccess,
		stopToolName:          e.stopToolName,
		stopToolReminder:      e.stopToolReminder,
		toolChoice:            e.toolChoice,
		requestItems:          e.currentTimeItems(req.CurrentTimeLocation),
	})
}

func (e *Engine) RunStream(ctx context.Context, req RunRequest, handler StreamHandler) (*RunResult, error) {
	if err := validateRunRequest(req); err != nil {
		return nil, err
	}
	session := req.Session
	if session == nil {
		session = NewSession()
	}
	if e.instructions != "" && len(session.Items) == 0 {
		session.AppendItems(model.SystemMessageItem{
			Content: []model.TextPart{{Text: e.instructions}},
		})
	}
	if req.Input != "" {
		session.AppendUserText(req.Input)
	}
	if len(req.InputParts) > 0 {
		session.AppendUserContent(req.InputParts...)
	}
	return runStream(ctx, e.client, session, req.Metadata, handler, runConfig{
		tools:                 e.tools,
		hooks:                 e.hooks,
		maxModelCalls:         e.maxModelCalls,
		parallelToolExecution: e.parallelToolExecution,
		maxToolConcurrency:    e.maxToolConcurrency,
		stopOnToolSuccess:     e.stopOnToolSuccess,
		stopToolName:          e.stopToolName,
		stopToolReminder:      e.stopToolReminder,
		toolChoice:            e.toolChoice,
		requestItems:          e.currentTimeItems(req.CurrentTimeLocation),
	})
}

func (e *Engine) currentTimeItems(override *time.Location) []model.InputItem {
	if e.currentTime == nil {
		return nil
	}
	location := override
	if location == nil {
		location = e.currentTime.location
	}
	current := e.currentTime.now().In(location)
	text := fmt.Sprintf("Current date and time: %s (%s).", current.Format(time.RFC3339), location.String())
	return []model.InputItem{
		model.SystemMessageItem{
			Content: []model.TextPart{model.NewTextPart(text)},
		},
	}
}

func validateRunRequest(req RunRequest) error {
	if req.Input != "" && len(req.InputParts) > 0 {
		return newEngineError(EngineErrorInvalidRequest, "run_request", fmt.Errorf("input and input_parts cannot both be set"))
	}
	return nil
}
