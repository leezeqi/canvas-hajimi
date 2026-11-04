package protocol

import (
	"context"
	"testing"
)

func TestOfficialResponsesNestedOutputText(t *testing.T) {
	adapter := officialPackageAdapter(t, "openai-responses.yingce-plugin", "openai-response")
	agent := adapter.(AgentAdapter)
	for _, tc := range []struct {
		name, body, want string
		tools            int
	}{
		{"top level wins", `{"output_text":"preferred","output":[{"type":"message","content":[{"type":"output_text","text":"duplicate"}]}]}`, "preferred", 0},
		{"blank message before final answer", `{"output":[{"type":"message","content":[{"type":"output_text","text":" "}]},{"type":"reasoning","encrypted_content":"private","summary":[{"text":"private reasoning"}]},{"type":"message","phase":"final_answer","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":14,"output_tokens":22}}`, "OK", 0},
		{"all parts retain whitespace", `{"output":[{"type":"message","content":[{"type":"output_text","text":"Hello "},{"type":"output_text","text":"\n"},{"type":"output_text","text":"  world"}]},{"type":"message","content":[{"type":"output_text","text":"!"}]}]}`, "Hello \n  world!", 0},
		{"tool calls are separate", `{"output":[{"type":"function_call","call_id":"call-1","name":"lookup","arguments":"{}","text":"not body"},{"type":"message","content":[{"type":"output_text","text":"answer"}]}]}`, "answer", 1},
		{"reasoning is not body", `{"output":[{"type":"reasoning","text":"private","summary":[{"text":"private"}]}]}`, "", 0},
		{"blank output is empty", `{"output":[{"type":"message","content":[{"type":"output_text","text":" \n "}]}]}`, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := adapter.ParseCreate(context.Background(), []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			text := ""
			if result.Result != nil {
				text = result.Result.Text
			}
			if text != tc.want {
				t.Fatalf("create text = %q, want %q", text, tc.want)
			}
			parsed, err := agent.ParseAgent(context.Background(), []byte(tc.body))
			if err != nil || parsed.Text != tc.want || len(parsed.ToolCalls) != tc.tools {
				t.Fatalf("agent result = %#v, %v", parsed, err)
			}
		})
	}
}

func TestManifestJoin(t *testing.T) {
	for _, tc := range []struct {
		name    string
		operand any
		want    string
		invalid bool
	}{
		{"join strings", []any{[]any{"a", "b"}, "\n"}, "a\nb", false},
		{"keep whitespace", []any{[]any{"a ", "\n", " b"}, ""}, "a \n b", false},
		{"empty list", []any{[]any{}, ""}, "", false},
		{"missing list", []any{map[string]any{"$ref": "missing"}, ""}, "", false},
		{"invalid list", []any{"not array", ""}, "", true},
		{"invalid item", []any{[]any{map[string]any{"text": "private"}}, ""}, "", true},
		{"invalid arguments", []any{}, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := evaluateManifestValue(map[string]any{"$join": tc.operand}, map[string]any{})
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid join succeeded")
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("join = %#v, %v; want %q", got, err, tc.want)
			}
		})
	}
}
