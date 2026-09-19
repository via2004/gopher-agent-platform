package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gopherai/internal/llm"
)

type fakeModel struct {
	planning      *llm.ToolModelResult
	planningErr   error
	final         *llm.Result
	finalErr      error
	stream        *llm.Result
	streamErr     error
	info          llm.ModelInfo
	planningTools []llm.ToolDefinition
	finalMessages []llm.Message
	finalState    []json.RawMessage
	finalCall     llm.ToolCall
	finalOutput   json.RawMessage
	streamCalls   int
}

func (f *fakeModel) GenerateWithTools(_ context.Context, _ []llm.Message, tools []llm.ToolDefinition) (*llm.ToolModelResult, error) {
	f.planningTools = tools
	return f.planning, f.planningErr
}

func (f *fakeModel) GenerateWithToolResult(_ context.Context, messages []llm.Message, continuation []json.RawMessage, call llm.ToolCall, output json.RawMessage) (*llm.Result, error) {
	f.finalMessages = messages
	f.finalState = continuation
	f.finalCall = call
	f.finalOutput = output
	return f.final, f.finalErr
}

func (f *fakeModel) GenerateStream(_ context.Context, _ []llm.Message, onDelta func(string) error) (*llm.Result, error) {
	f.streamCalls++
	if onDelta != nil {
		_ = onDelta("delta")
	}
	return f.stream, f.streamErr
}

func (f *fakeModel) Info() llm.ModelInfo { return f.info }

type fakeTools struct {
	definitions []llm.ToolDefinition
	output      json.RawMessage
	err         error
	calls       int
	name        string
	arguments   json.RawMessage
	ctx         context.Context
}

type fakeStreamingModel struct {
	fakeModel
	streamPlanning       *llm.ToolModelResult
	streamPlanningErr    error
	streamPlanningDeltas []string
	streamFinal          *llm.Result
	streamFinalErr       error
	streamCall           llm.ToolCall
	streamOutput         json.RawMessage
	streamPlanningCtx    context.Context
	streamFinalCtx       context.Context
}

func (f *fakeStreamingModel) GenerateStreamWithTools(ctx context.Context, _ []llm.Message, _ []llm.ToolDefinition, onDelta func(string) error) (*llm.ToolModelResult, error) {
	f.streamPlanningCtx = ctx
	for _, delta := range f.streamPlanningDeltas {
		if err := onDelta(delta); err != nil {
			return nil, err
		}
	}
	return f.streamPlanning, f.streamPlanningErr
}

func (f *fakeStreamingModel) GenerateStreamWithToolResult(ctx context.Context, _ []llm.Message, _ []json.RawMessage, call llm.ToolCall, output json.RawMessage, onDelta func(string) error) (*llm.Result, error) {
	f.streamFinalCtx = ctx
	f.streamCall = call
	f.streamOutput = output
	if f.streamFinalErr != nil {
		return nil, f.streamFinalErr
	}
	if err := onDelta("final "); err != nil {
		return nil, err
	}
	if err := onDelta("answer"); err != nil {
		return nil, err
	}
	return f.streamFinal, nil
}

func (f *fakeTools) Tools() []llm.ToolDefinition { return f.definitions }

func (f *fakeTools) Call(ctx context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	f.calls++
	f.ctx = ctx
	f.name = name
	f.arguments = arguments
	return f.output, f.err
}

func TestClientGenerateReturnsPlanningTextWithoutToolCall(t *testing.T) {
	want := &llm.Result{Content: "plain answer", TotalTokens: 7}
	model := &fakeModel{planning: &llm.ToolModelResult{Result: want}}
	tools := &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}}
	client, err := NewClient(model, tools)
	if err != nil {
		t.Fatal(err)
	}

	got, err := client.Generate(t.Context(), []llm.Message{{Role: "user", Content: "hello"}})
	if err != nil || got != want {
		t.Fatalf("Generate() = %#v, %v", got, err)
	}
	if tools.calls != 0 || len(model.planningTools) != 1 {
		t.Fatalf("tool calls = %d, planning tools = %#v", tools.calls, model.planningTools)
	}
}

func TestClientGenerateExecutesToolAndAggregatesUsage(t *testing.T) {
	call := llm.ToolCall{ID: "fc_1", CallID: "call_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"上海"}`)}
	continuation := []json.RawMessage{json.RawMessage(`{"type":"function_call"}`)}
	model := &fakeModel{
		planning: &llm.ToolModelResult{
			Result:       &llm.Result{InputTokens: 10, OutputTokens: 3, TotalTokens: 13},
			ToolCalls:    []llm.ToolCall{call},
			Continuation: continuation,
		},
		final: &llm.Result{Content: "上海现在晴", Model: "gpt-final", InputTokens: 20, OutputTokens: 5, TotalTokens: 25},
	}
	toolOutput := json.RawMessage(`{"location":"上海","condition":"晴"}`)
	tools := &fakeTools{
		definitions: []llm.ToolDefinition{{Name: "get_weather"}},
		output:      toolOutput,
	}
	client, err := NewClient(model, tools)
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{{Role: "user", Content: "上海天气"}}

	got, err := client.Generate(t.Context(), messages)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "上海现在晴" || got.InputTokens != 30 || got.OutputTokens != 8 || got.TotalTokens != 38 {
		t.Fatalf("Generate() = %#v", got)
	}
	if tools.calls != 1 || tools.name != call.Name || string(tools.arguments) != string(call.Arguments) {
		t.Fatalf("tool call = %d, %s, %s", tools.calls, tools.name, tools.arguments)
	}
	if model.finalCall.ID != call.ID || model.finalCall.CallID != call.CallID || model.finalCall.Name != call.Name ||
		string(model.finalCall.Arguments) != string(call.Arguments) || string(model.finalOutput) != string(toolOutput) ||
		len(model.finalState) != 1 || len(model.finalMessages) != 1 {
		t.Fatalf("final model input = call %#v output %s state %#v messages %#v", model.finalCall, model.finalOutput, model.finalState, model.finalMessages)
	}
}

func TestClientGenerateRejectsMultipleToolCalls(t *testing.T) {
	model := &fakeModel{planning: &llm.ToolModelResult{
		Result:    &llm.Result{},
		ToolCalls: []llm.ToolCall{{Name: "one"}, {Name: "two"}},
	}}
	tools := &fakeTools{}
	client, err := NewClient(model, tools)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(t.Context(), nil); !errors.Is(err, ErrMultipleToolCalls) {
		t.Fatalf("Generate() error = %v", err)
	}
	if tools.calls != 0 {
		t.Fatalf("tool calls = %d, want 0", tools.calls)
	}
}

func TestClientGeneratePropagatesStageErrors(t *testing.T) {
	planningErr := errors.New("planning failed")
	toolErr := errors.New("tool failed")
	finalErr := errors.New("final failed")
	tests := []struct {
		name    string
		model   *fakeModel
		tools   *fakeTools
		wantErr error
	}{
		{name: "planning", model: &fakeModel{planningErr: planningErr}, tools: &fakeTools{}, wantErr: planningErr},
		{name: "invalid planning", model: &fakeModel{}, tools: &fakeTools{}, wantErr: ErrInvalidPlanning},
		{name: "tool", model: toolPlanning(&llm.Result{}), tools: &fakeTools{err: toolErr}, wantErr: toolErr},
		{name: "final", model: func() *fakeModel { m := toolPlanning(nil); m.finalErr = finalErr; return m }(), tools: &fakeTools{output: json.RawMessage(`{}`)}, wantErr: finalErr},
		{name: "invalid final", model: toolPlanning(nil), tools: &fakeTools{output: json.RawMessage(`{}`)}, wantErr: ErrInvalidFinalResult},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, err := NewClient(test.model, test.tools)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.Generate(t.Context(), nil); !errors.Is(err, test.wantErr) {
				t.Fatalf("Generate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestClientDelegatesStreamingAndModelInfo(t *testing.T) {
	want := &llm.Result{Content: "streamed"}
	model := &fakeModel{stream: want, info: llm.ModelInfo{Provider: "openai", Model: "gpt-test"}}
	client, err := NewClient(model, &fakeTools{})
	if err != nil {
		t.Fatal(err)
	}
	var delta string
	got, err := client.GenerateStream(t.Context(), nil, func(value string) error { delta = value; return nil })
	if err != nil || got != want || delta != "delta" || model.streamCalls != 1 {
		t.Fatalf("GenerateStream() = %#v, %v, delta %q, calls %d", got, err, delta, model.streamCalls)
	}
	if info := client.Info(); info != model.info {
		t.Fatalf("Info() = %#v", info)
	}
}

func TestClientGenerateStreamRejectsMissingCallback(t *testing.T) {
	client, err := NewClient(&fakeStreamingModel{}, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.GenerateStream(t.Context(), nil, nil); !errors.Is(err, llm.ErrOnDeltaMissed) {
		t.Fatalf("GenerateStream() error = %v, want %v", err, llm.ErrOnDeltaMissed)
	}
}

func TestClientGenerateStreamFlushesPlanningDeltasWithoutToolCall(t *testing.T) {
	model := &fakeStreamingModel{
		streamPlanning:       &llm.ToolModelResult{Result: &llm.Result{Content: "plain answer"}},
		streamPlanningDeltas: []string{"plain ", "answer"},
	}
	client, err := NewClient(model, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	result, err := client.GenerateStream(t.Context(), nil, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "plain answer" || strings.Join(deltas, "") != result.Content {
		t.Fatalf("GenerateStream() = %#v, deltas = %#v", result, deltas)
	}
	if len(deltas) != 2 || deltas[0] != "plain " || deltas[1] != "answer" {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestClientGenerateStreamExecutesToolAndAggregatesUsage(t *testing.T) {
	call := llm.ToolCall{ID: "fc_1", CallID: "call_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"上海"}`)}
	toolOutput := json.RawMessage(`{"location":"上海","temperature_c":23}`)
	model := &fakeStreamingModel{
		streamPlanning: &llm.ToolModelResult{
			Result:       &llm.Result{InputTokens: 10, OutputTokens: 3, TotalTokens: 13},
			ToolCalls:    []llm.ToolCall{call},
			Continuation: []json.RawMessage{json.RawMessage(`{"type":"function_call"}`)},
		},
		streamPlanningDeltas: []string{"I will check the weather."},
		streamFinal:          &llm.Result{Content: "final answer", InputTokens: 20, OutputTokens: 5, TotalTokens: 25},
	}
	tools := &fakeTools{output: toolOutput, definitions: []llm.ToolDefinition{{Name: "get_weather"}}}
	client, err := NewClient(model, tools)
	if err != nil {
		t.Fatal(err)
	}
	var deltas []string
	type contextKey string
	ctx := context.WithValue(t.Context(), contextKey("request-id"), "request-1")
	result, err := client.GenerateStream(ctx, []llm.Message{{Role: "user", Content: "上海天气"}}, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "final answer" || result.InputTokens != 30 || result.OutputTokens != 8 || result.TotalTokens != 38 {
		t.Fatalf("GenerateStream() = %#v", result)
	}
	if len(deltas) != 2 || deltas[0] != "final " || deltas[1] != "answer" || strings.Join(deltas, "") != result.Content {
		t.Fatalf("deltas = %#v", deltas)
	}
	if tools.calls != 1 || model.streamCall.Name != call.Name || string(model.streamOutput) != string(toolOutput) {
		t.Fatalf("tool call = %d, %s, %s", tools.calls, model.streamCall.Name, model.streamOutput)
	}
	if model.streamPlanningCtx != ctx || tools.ctx != ctx || model.streamFinalCtx != ctx {
		t.Fatal("GenerateStream() did not pass the request context through planning, tool call, and final response")
	}
}

func TestClientGenerateStreamDoesNotExposePlanningDeltasOnToolFailure(t *testing.T) {
	toolErr := errors.New("tool failed")
	model := &fakeStreamingModel{
		streamPlanning: &llm.ToolModelResult{
			Result:    &llm.Result{Content: "I will use a tool."},
			ToolCalls: []llm.ToolCall{{Name: "get_weather", Arguments: json.RawMessage(`{}`)}},
		},
		streamPlanningDeltas: []string{"I will use a tool."},
	}
	client, err := NewClient(model, &fakeTools{
		definitions: []llm.ToolDefinition{{Name: "get_weather"}},
		err:         toolErr,
	})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	result, err := client.GenerateStream(t.Context(), nil, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if !errors.Is(err, toolErr) || result != nil {
		t.Fatalf("GenerateStream() = %#v, %v; want tool error", result, err)
	}
	if len(deltas) != 0 {
		t.Fatalf("planning deltas were exposed: %#v", deltas)
	}
}

func TestClientGenerateStreamDoesNotExposePlanningDeltasForMultipleToolCalls(t *testing.T) {
	model := &fakeStreamingModel{
		streamPlanning: &llm.ToolModelResult{
			Result:    &llm.Result{Content: "planning"},
			ToolCalls: []llm.ToolCall{{Name: "one"}, {Name: "two"}},
		},
		streamPlanningDeltas: []string{"planning"},
	}
	client, err := NewClient(model, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
	if err != nil {
		t.Fatal(err)
	}

	var deltas []string
	result, err := client.GenerateStream(t.Context(), nil, func(delta string) error {
		deltas = append(deltas, delta)
		return nil
	})
	if !errors.Is(err, ErrMultipleToolCalls) || result != nil {
		t.Fatalf("GenerateStream() = %#v, %v; want %v", result, err, ErrMultipleToolCalls)
	}
	if len(deltas) != 0 {
		t.Fatalf("planning deltas were exposed: %#v", deltas)
	}
}

func TestClientGenerateStreamBoundsPlanningOutput(t *testing.T) {
	tests := []struct {
		name    string
		delta   string
		wantErr error
	}{
		{name: "at limit", delta: strings.Repeat("界", maxPlanningOutputRunes)},
		{name: "over limit", delta: strings.Repeat("界", maxPlanningOutputRunes+1), wantErr: ErrPlanningOutputTooLarge},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := &fakeStreamingModel{
				streamPlanning:       &llm.ToolModelResult{Result: &llm.Result{Content: test.delta}},
				streamPlanningDeltas: []string{test.delta},
			}
			client, err := NewClient(model, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
			if err != nil {
				t.Fatal(err)
			}
			var deltas []string
			result, err := client.GenerateStream(t.Context(), nil, func(delta string) error {
				deltas = append(deltas, delta)
				return nil
			})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("GenerateStream() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				if result != nil || len(deltas) != 0 {
					t.Fatalf("over-limit result = %#v, deltas = %#v", result, deltas)
				}
				return
			}
			if result == nil || len(deltas) != 1 || deltas[0] != test.delta {
				t.Fatalf("at-limit result = %#v, deltas = %#v", result, deltas)
			}
		})
	}
}

func TestClientGenerateStreamStopsBufferingWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := &fakeStreamingModel{
		streamPlanning:       &llm.ToolModelResult{Result: &llm.Result{Content: "planning"}},
		streamPlanningDeltas: []string{"planning"},
	}
	client, err := NewClient(model, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.GenerateStream(ctx, nil, func(string) error {
		t.Fatal("external callback was called")
		return nil
	})
	if !errors.Is(err, context.Canceled) || result != nil {
		t.Fatalf("GenerateStream() = %#v, %v; want context canceled", result, err)
	}
}

func TestClientGenerateStreamPropagatesCallbackErrorWhenFlushingPlanning(t *testing.T) {
	callbackErr := errors.New("callback failed")
	model := &fakeStreamingModel{
		streamPlanning:       &llm.ToolModelResult{Result: &llm.Result{Content: "answer"}},
		streamPlanningDeltas: []string{"answer"},
	}
	client, err := NewClient(model, &fakeTools{definitions: []llm.ToolDefinition{{Name: "get_weather"}}})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.GenerateStream(t.Context(), nil, func(string) error { return callbackErr })
	if !errors.Is(err, callbackErr) || result != nil {
		t.Fatalf("GenerateStream() = %#v, %v; want callback error", result, err)
	}
}

func TestNewClientRejectsInvalidDependencies(t *testing.T) {
	if _, err := NewClient(nil, &fakeTools{}); !errors.Is(err, ErrInvalidModel) {
		t.Fatalf("nil model error = %v", err)
	}
	if _, err := NewClient(&fakeModel{}, nil); !errors.Is(err, ErrInvalidToolExecutor) {
		t.Fatalf("nil tools error = %v", err)
	}
}

func toolPlanning(final *llm.Result) *fakeModel {
	return &fakeModel{
		planning: &llm.ToolModelResult{
			Result:       &llm.Result{},
			ToolCalls:    []llm.ToolCall{{ID: "fc", CallID: "call", Name: "get_weather", Arguments: json.RawMessage(`{}`)}},
			Continuation: []json.RawMessage{json.RawMessage(`{}`)},
		},
		final: final,
	}
}
