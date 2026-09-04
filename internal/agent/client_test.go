package agent

import (
	"context"
	"encoding/json"
	"errors"
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
}

func (f *fakeTools) Tools() []llm.ToolDefinition { return f.definitions }

func (f *fakeTools) Call(_ context.Context, name string, arguments json.RawMessage) (json.RawMessage, error) {
	f.calls++
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
