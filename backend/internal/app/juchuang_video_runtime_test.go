package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/protocol"
)

func TestJuchuangRuntimeUploadSubmitAndResume(t *testing.T) {
	data, err := os.ReadFile("../../../plugin-packages/juchuang-video/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := protocol.LoadInstalledPlugin(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	uploads, creates, polls := 0, 0, 0
	missingURL := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing upload/create/poll auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/files":
			uploads++
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				http.Error(w, "missing file", 400)
				return
			}
			file.Close()
			if header.Size == 0 {
				t.Error("empty file")
			}
			w.WriteHeader(http.StatusCreated)
			if missingURL {
				w.Write([]byte(`{"id":"file-no-url"}`))
				return
			}
			w.Write([]byte(`{"id":"file-1","url":"https://cdn.example/reference.png"}`))
		case "/v1/media/generate":
			creates++
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			params := body["params"].(map[string]any)
			images := params["images"].([]any)
			if body["client_request_id"] != "stable-task-key" || len(images) != 1 || images[0] != "https://cdn.example/reference.png" {
				t.Errorf("bad create: %#v", body)
			}
			w.Write([]byte(`{"code":200,"data":{"task_id":"task-juchuang","status":"pending","is_final":false}}`))
		case "/v1/tasks/task-juchuang":
			polls++
			w.Write([]byte(`{"id":"task-juchuang","status":"failed","is_final":true,"error":"参考素材不符合上游模型要求"}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	input := canvasGenerationInput{Mode: "video", Prompt: "A quiet landscape", Config: providerConfig{BaseURL: server.URL + "/v1", APIKey: "test-key", Model: "seedance-2-0-special", InterfaceType: "juchuang-video", VideoSeconds: "15", VQuality: "720p", Size: "16:9"}, ReferenceImages: []providerMedia{{DataURL: "data:image/png;base64,aW1hZ2U=", Role: "reference_image"}}, Metadata: map[string]any{"videoEditOperation": "reference_to_video"}}
	ctx := context.WithValue(context.Background(), providerSubmissionKeyContext{}, "stable-task-key")
	_, err = runProtocolAdapterTaskWithPolicy(ctx, input, adapter, defaultVideoPollPolicy())
	var pending providerStatePendingError
	if !errors.As(err, &pending) || pending.TaskID != "task-juchuang" || uploads != 1 || creates != 1 {
		t.Fatalf("create: uploads=%d creates=%d err=%v", uploads, creates, err)
	}
	// Resume uses the existing upstream task and must never create a second billable task.
	resume := context.WithValue(ctx, providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: pending.TaskID})
	_, err = runProtocolAdapterTaskWithPolicy(resume, input, adapter, defaultVideoPollPolicy())
	if err == nil || uploads != 1 || creates != 1 || polls != 1 {
		t.Fatalf("resume: uploads=%d creates=%d polls=%d err=%v", uploads, creates, polls, err)
	}
	missingURL = true
	_, err = runProtocolAdapterTaskWithPolicy(ctx, input, adapter, defaultVideoPollPolicy())
	if err == nil || creates != 1 || !strings.Contains(providerUserFacingErrorMessage(err), "素材标识或链接") {
		t.Fatalf("invalid upload must stop create: %v", err)
	}
}

func TestTaskSummaryReturnsSafeFailureReason(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{"", "生成失败"},
		{"参考素材不符合模型要求", "参考素材不符合模型要求"},
		{`模型服务拒绝了请求；上游：{"message":"Channel 177 requires public HTTPS reference URLs; token=private-secret https://private.example/a"}`, "公网 HTTPS 链接"},
	} {
		summary := taskSummaryForOutput(model.Task{Status: model.TaskStatusFailed, Error: tc.raw})
		if !strings.Contains(summary.Error, tc.want) || strings.Contains(summary.Error, "private-secret") || strings.Contains(summary.Error, "private.example") || strings.Contains(summary.Error, "Channel 177") {
			t.Fatalf("unsafe/missing reason: %q", summary.Error)
		}
		encoded, _ := json.Marshal(summary)
		if !strings.Contains(string(encoded), `"error":`) {
			t.Fatal("list response omitted error")
		}
	}
	if summary := taskSummaryForOutput(model.Task{Status: model.TaskStatusRunning}); summary.Error != "" {
		t.Fatalf("running task has error %q", summary.Error)
	}
	err := providerHTTPError{StatusCode: 400, Body: `{"code":"plugin_request_invalid","message":"Channel 177 requires public HTTPS reference URLs; token=private-secret"}`}
	if message := providerUserFacingErrorMessage(err); !strings.Contains(message, "公网 HTTPS 链接") || strings.Contains(message, "private-secret") {
		t.Fatalf("provider reason = %q", message)
	}
}
