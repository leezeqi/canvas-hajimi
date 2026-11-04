package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"yingce/backend/internal/protocol"
)

// 傲天特价线路拒绝 multipart 文件，运行时必须以 JSON 提交公网参考图 URL，且续查不重复提交。
func TestAotianRuntimeSubmitsJSONReferenceURLsAndResumesExistingTask(t *testing.T) {
	data, err := os.ReadFile("../../../plugin-packages/aotian-video/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := protocol.LoadInstalledPlugin(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	const referenceURL = "https://canvas.example/api/public/resources/ref/file?expires=1&signature=s&variant=original"
	creates, polls := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing bearer auth")
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/videos":
			creates++
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				t.Errorf("create content type = %q", r.Header.Get("Content-Type"))
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			want := map[string]any{"model": "特价seedance-2.0-720p", "prompt": "民国赌场", "seconds": "12", "size": "16:9", "resolution": "720p", "images": []any{referenceURL}}
			if !reflect.DeepEqual(body, want) {
				t.Errorf("create body = %#v", body)
			}
			w.Write([]byte(`{"id":"task_aotian","object":"video","status":"queued","progress":0}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/videos/task_aotian":
			polls++
			w.Write([]byte(`{"id":"task_aotian","status":"failed","error":{"message":"参考图无法下载","code":"video_generation_failed"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	input := canvasGenerationInput{
		Mode:            "video",
		Prompt:          "民国赌场",
		Config:          providerConfig{BaseURL: server.URL, APIKey: "test-key", Model: "特价seedance-2.0-720p", InterfaceType: "aotian-video", VideoSeconds: "12", VQuality: "720p", Size: "16:9"},
		ReferenceImages: []providerMedia{{URL: referenceURL, Role: "reference_image", MimeType: "image/png"}},
		Metadata:        map[string]any{"videoEditOperation": "reference_to_video"},
	}
	ctx := context.WithValue(context.Background(), providerSubmissionKeyContext{}, "stable-task-key")
	_, err = runProtocolAdapterTaskWithPolicy(ctx, input, adapter, defaultVideoPollPolicy())
	var pending providerStatePendingError
	if !errors.As(err, &pending) || pending.TaskID != "task_aotian" || creates != 1 || polls != 0 {
		t.Fatalf("create: creates=%d polls=%d err=%v", creates, polls, err)
	}
	resume := context.WithValue(ctx, providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: pending.TaskID})
	_, err = runProtocolAdapterTaskWithPolicy(resume, input, adapter, defaultVideoPollPolicy())
	if err == nil || creates != 1 || polls != 1 || !strings.Contains(err.Error(), "参考图无法下载") {
		t.Fatalf("resume: creates=%d polls=%d err=%v", creates, polls, err)
	}

	embedded := input
	embedded.ReferenceImages = []providerMedia{{DataURL: "data:image/png;base64,aW1hZ2U=", Role: "reference_image"}}
	_, err = runProtocolAdapterTaskWithPolicy(ctx, embedded, adapter, defaultVideoPollPolicy())
	if err == nil || creates != 1 || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("embedded reference must stop before create: creates=%d err=%v", creates, err)
	}
}
