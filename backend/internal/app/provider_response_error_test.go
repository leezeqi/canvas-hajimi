package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"yingce/backend/internal/protocol"
)

func TestTextResponseErrorsAreNotNetworkErrors(t *testing.T) {
	for _, result := range []*protocol.Result{nil, {}, {Text: " \n "}, {Reasoning: "private reasoning"}} {
		_, err := finishProtocolResult(context.Background(), providerConfig{}, "text", "", result, videoPollPolicy{})
		if err == nil {
			t.Fatal("empty body succeeded")
		}
		message := providerUserFacingErrorMessage(err)
		if !strings.Contains(message, "未返回可用正文") || strings.Contains(message, "连接") || strings.Contains(message, "private") {
			t.Fatalf("empty body message = %q", message)
		}
	}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"decode", fmt.Errorf("decode: %w", errors.New("private raw response")), "响应处理失败"},
		{"network", &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("private upstream")}, "连接模型服务失败"},
		{"http", providerHTTPError{StatusCode: http.StatusServiceUnavailable}, "HTTP 503"},
		{"timeout", context.DeadlineExceeded, "响应超时"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := providerUserFacingErrorMessage(tc.err)
			if !strings.Contains(message, tc.want) || strings.Contains(message, "private") {
				t.Fatalf("message = %q, want category %q", message, tc.want)
			}
		})
	}
}

func TestResponsesSynchronousGenerationReadsNestedOutput(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":" "}]},{"type":"reasoning","summary":[]},{"type":"message","content":[{"type":"output_text","text":"OK"}]}]}`)
	}))
	defer server.Close()
	result, err := runTextTask(withProtocolRegistry(context.Background(), loadOfficialFallbackRegistry()), canvasGenerationInput{
		Mode: "text", Prompt: "Reply with OK.",
		Config: providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: "test-model", InterfaceType: "openai-response"},
	})
	if err != nil || result["text"] != "OK" {
		t.Fatalf("generation = %#v, %v", result, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("upstream calls = %d, want exactly one", calls.Load())
	}
}
