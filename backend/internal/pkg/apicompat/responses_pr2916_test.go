package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// normalizeResponsesArguments 归一化 function_call.arguments：
// JSON 字符串形态解包、对象形态透传、非法 JSON 一律降级为 {}。
func TestResponses2916NormalizeArguments(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "stringified object", raw: json.RawMessage(`"{\"cmd\":\"ls\"}"`), want: `{"cmd":"ls"}`},
		{name: "raw object", raw: json.RawMessage(`{"cmd":"ls"}`), want: `{"cmd":"ls"}`},
		{name: "empty string", raw: json.RawMessage(`""`), want: `{}`},
		{name: "invalid string", raw: json.RawMessage(`"not json"`), want: `{}`},
		{name: "null", raw: json.RawMessage(`null`), want: `{}`},
		{name: "empty", raw: nil, want: `{}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.JSONEq(t, tt.want, string(normalizeResponsesArguments(tt.raw)))
		})
	}
}

// extractResponsesOutputText 把 function_call_output.output 归一化为纯文本。
func TestResponses2916ExtractOutputText(t *testing.T) {
	assert.Equal(t, "ok", extractResponsesOutputText(json.RawMessage(`"ok"`)))
	assert.Equal(t, "one\n\ntwo", extractResponsesOutputText(json.RawMessage(`[
		{"type":"output_text","text":"one"},
		{"type":"output_text","text":"two"}
	]`)))
	assert.Equal(t, "", extractResponsesOutputText(nil))
}

// ResponsesInputItem 必须接受对象形态的 arguments（Codex 等新客户端直接给对象），
// 并在转成 Anthropic tool_use.input 时保持为 JSON 对象。
//
// 注意：本仓库的 normalizeAnthropicToolPairing 会丢弃没有配对 tool_result 的
// tool_use（否则上游 400），因此这里必须带上 function_call_output 才能观察到 tool_use。
func TestResponses2916ToAnthropicObjectArguments(t *testing.T) {
	var req ResponsesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-test",
		"input":[
			{"type":"function_call","call_id":"call_1","name":"exec","arguments":{"cmd":"ls"}},
			{"type":"function_call_output","call_id":"call_1","output":"done"}
		]
	}`), &req)
	require.NoError(t, err)

	anth, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	require.Len(t, anth.Messages, 2)
	assertAnthropicPairing(t, anth.Messages)

	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anth.Messages[0].Content, &blocks))
	require.Len(t, blocks, 1)
	assert.Equal(t, "tool_use", blocks[0].Type)
	assert.JSONEq(t, `{"cmd":"ls"}`, string(blocks[0].Input))
}

// 字符串形态的 arguments 与对象形态必须产出同样的 tool_use.input。
func TestResponses2916ToAnthropicStringArgumentsMatchObjectForm(t *testing.T) {
	var req ResponsesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-test",
		"input":[
			{"type":"function_call","call_id":"call_1","name":"exec","arguments":"{\"cmd\":\"ls\"}"},
			{"type":"function_call_output","call_id":"call_1","output":"done"}
		]
	}`), &req)
	require.NoError(t, err)

	anth, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	require.Len(t, anth.Messages, 2)

	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anth.Messages[0].Content, &blocks))
	require.Len(t, blocks, 1)
	assert.JSONEq(t, `{"cmd":"ls"}`, string(blocks[0].Input))
}

// function_call_output.output 为分片数组时必须转成 tool_result 的文本内容。
func TestResponses2916ToAnthropicOutputArray(t *testing.T) {
	var req ResponsesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-test",
		"input":[
			{"type":"function_call","call_id":"call_1","name":"exec","arguments":{}},
			{"type":"function_call_output","call_id":"call_1","output":[{"type":"output_text","text":"done"}]}
		]
	}`), &req)
	require.NoError(t, err)

	anth, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	require.Len(t, anth.Messages, 2)

	var blocks []AnthropicContentBlock
	require.NoError(t, json.Unmarshal(anth.Messages[1].Content, &blocks))
	require.Len(t, blocks, 1)
	require.Equal(t, "tool_result", blocks[0].Type)
	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(blocks[0].Content, &parts))
	require.Len(t, parts, 1)
	assert.Equal(t, "done", parts[0].Text)
}

// instructions 与 developer 消息都要合并进 system（本仓库的 system 为 JSON 字符串）。
func TestResponses2916InstructionsAndDeveloperBecomeSystem(t *testing.T) {
	var req ResponsesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-test",
		"instructions":"top system",
		"input":[
			{"role":"developer","content":"dev system"},
			{"role":"user","content":"hello"}
		]
	}`), &req)
	require.NoError(t, err)

	anth, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	assert.Equal(t, "top system\n\ndev system", systemText2916(t, anth.System))
}

func TestResponses2916EmptySystemOmitted(t *testing.T) {
	var req ResponsesRequest
	err := json.Unmarshal([]byte(`{
		"model":"claude-test",
		"instructions":"   ",
		"input":[{"role":"user","content":"hello"}]
	}`), &req)
	require.NoError(t, err)

	anth, err := ResponsesToAnthropicRequest(&req)
	require.NoError(t, err)
	assert.Empty(t, anth.System)
}

// function 工具去掉 type、server 工具（web_search）带上 Anthropic 的版本化 type
// 且不带 input_schema。
func TestResponses2916ToolSchemaAndWebSearch(t *testing.T) {
	req := &ResponsesRequest{
		Model: "claude-test",
		Input: json.RawMessage(`[{"role":"user","content":"hello"}]`),
		Tools: []ResponsesTool{
			{Type: "function", Name: "run"},
			{Type: "web_search"},
		},
	}

	anth, err := ResponsesToAnthropicRequest(req)
	require.NoError(t, err)
	require.Len(t, anth.Tools, 2)
	assert.Empty(t, anth.Tools[0].Type)
	assert.JSONEq(t, `{"type":"object","properties":{}}`, string(anth.Tools[0].InputSchema))
	assert.Equal(t, "web_search_20250305", anth.Tools[1].Type)
	assert.Equal(t, "web_search", anth.Tools[1].Name)
	assert.Empty(t, anth.Tools[1].InputSchema)
}

// Anthropic → Responses 流：文本块必须带出 content_part.added/done 且 done 携带全文。
func TestResponses2916AnthropicStreamMessageDoneCarriesContent(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	var all []ResponsesStreamEvent
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_1",
			Model: "claude-test",
		},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "text"},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "text_delta", Text: "hello"},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop"}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "message_stop"}, state)...)

	assert.NotNil(t, findEvent2916(all, "response.content_part.added"))
	assert.NotNil(t, findEvent2916(all, "response.content_part.done"))
	done := findDoneItem2916(all, "message")
	require.NotNil(t, done)
	require.Len(t, done.Content, 1)
	assert.Equal(t, "hello", done.Content[0].Text)
}

// Anthropic → Responses 流：tool_use 的累计 arguments 必须出现在
// output_item.done 的 item 上（本仓库的 arguments.done 事件本身不带 arguments，
// 由 responses_stream_event_wire.go 在序列化时按 e.Arguments 输出）。
func TestResponses2916AnthropicStreamFunctionDoneCarriesCall(t *testing.T) {
	state := NewAnthropicEventToResponsesState()
	var all []ResponsesStreamEvent
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type: "message_start",
		Message: &AnthropicResponse{
			ID:    "msg_1",
			Model: "claude-test",
		},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:         "content_block_start",
		ContentBlock: &AnthropicContentBlock{Type: "tool_use", ID: "toolu_1", Name: "exec"},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{
		Type:  "content_block_delta",
		Delta: &AnthropicDelta{Type: "input_json_delta", PartialJSON: `{"cmd":"ls"}`},
	}, state)...)
	all = append(all, AnthropicEventToResponsesEvents(&AnthropicStreamEvent{Type: "content_block_stop"}, state)...)

	argsDelta := findEvent2916(all, "response.function_call_arguments.delta")
	require.NotNil(t, argsDelta)
	assert.Equal(t, `{"cmd":"ls"}`, argsDelta.Delta)
	require.NotNil(t, findEvent2916(all, "response.function_call_arguments.done"))

	done := findDoneItem2916(all, "function_call")
	require.NotNil(t, done)
	// 本仓库沿用上游的 tool_use.id 作为 call_id（不加 fc_ 前缀）。
	assert.Equal(t, "toolu_1", done.CallID)
	assert.Equal(t, "exec", done.Name)
	assert.Equal(t, `{"cmd":"ls"}`, done.Arguments)
}

func systemText2916(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	var parts []ResponsesContentPart
	require.NoError(t, json.Unmarshal(raw, &parts))
	var out string
	for _, p := range parts {
		out += p.Text
	}
	return out
}

func findEvent2916(events []ResponsesStreamEvent, typ string) *ResponsesStreamEvent {
	for i := range events {
		if events[i].Type == typ {
			return &events[i]
		}
	}
	return nil
}

func findDoneItem2916(events []ResponsesStreamEvent, typ string) *ResponsesOutput {
	for i := range events {
		if events[i].Type == "response.output_item.done" && events[i].Item != nil && events[i].Item.Type == typ {
			return events[i].Item
		}
	}
	return nil
}
