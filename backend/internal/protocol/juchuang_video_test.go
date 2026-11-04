package protocol

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestJuchuangVideoUploadAndCreate(t *testing.T) {
	adapter := officialPackageAdapter(t, "juchuang-video.yingce-plugin", "juchuang-video")
	if !adapter.Metadata().IdempotentCreate || !adapter.Metadata().DetachedPolling || adapter.Metadata().RequiresPublicMediaURLs {
		t.Fatalf("unexpected metadata: %+v", adapter.Metadata())
	}
	uploader := adapter.(MediaUploadAdapter)
	media := func(kind, role, name string, order int) MediaReference {
		return MediaReference{Kind: kind, Role: role, Name: name, Order: order, DataURL: "data:image/png;base64,AA==", Metadata: map[string]any{MediaUploadIDKey: "https://cdn.example/" + name}}
	}
	for _, kind := range []string{"image", "video", "audio"} {
		spec, err := uploader.BuildUpload(context.Background(), RequestContext{}, media(kind, "reference_image", "ref.png", 1))
		if err != nil || spec.Path != "/v1/files" || spec.Method != "POST" || spec.ContentType != "multipart/form-data" || len(spec.Files) != 1 || spec.Files[0].Name != "file" {
			t.Fatalf("upload spec: %+v, %v", spec, err)
		}
	}
	for _, body := range []string{`{"id":"file-only"}`, `{"url":"http://private.example/ref.png"}`, `{"url":"data:image/png;base64,AA=="}`} {
		if _, err := uploader.ParseUpload(context.Background(), []byte(body)); err == nil {
			t.Fatalf("invalid upload response accepted: %s", body)
		}
	}
	if value, err := uploader.ParseUpload(context.Background(), []byte(`{"id":"file-id","url":"https://cdn.example/ref.png"}`)); err != nil || value != "https://cdn.example/ref.png" {
		t.Fatalf("upload URL = %q, %v", value, err)
	}
	for _, tc := range []struct {
		name, operation, mode  string
		images, videos, audios []MediaReference
		wantImages             []any
	}{
		{name: "text", operation: "text_to_video", mode: "text-to-video"},
		{name: "first", operation: "image_to_video", mode: "first-frame", images: []MediaReference{media("image", "first_frame", "first.png", 1)}, wantImages: []any{"https://cdn.example/first.png"}},
		{name: "frames reversed", operation: "image_to_video", mode: "first-last", images: []MediaReference{media("image", "last_frame", "last.png", 1), media("image", "first_frame", "first.png", 2)}, wantImages: []any{"https://cdn.example/first.png", "https://cdn.example/last.png"}},
		{name: "multimodal", operation: "reference_to_video", mode: "reference", images: []MediaReference{media("image", "reference_image", "second.png", 2), media("image", "reference_image", "first.png", 1)}, videos: []MediaReference{media("video", "reference_video", "ref.mp4", 3)}, audios: []MediaReference{media("audio", "reference_audio", "ref.wav", 4)}, wantImages: []any{"https://cdn.example/first.png", "https://cdn.example/second.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := GenerationRequest{Model: "seedance-2-0-special", Prompt: "A quiet landscape", Duration: 15, Resolution: "720p", AspectRatio: "16:9", Operation: tc.operation, Images: tc.images, Videos: tc.videos, Audios: tc.audios, Extra: map[string]any{"idempotencyKey": "stable-task-key"}}
			request.Inputs = append(append(append([]MediaReference{}, tc.images...), tc.videos...), tc.audios...)
			spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
			if err != nil {
				t.Fatal(err)
			}
			if spec.Path != "/v1/media/generate" || spec.ContentType != "application/json" {
				t.Fatalf("create = %+v", spec)
			}
			body := manifestTestBody(t, spec)
			params := body["params"].(map[string]any)
			if body["client_request_id"] != "stable-task-key" || params["mode"] != tc.mode || params["duration"] != float64(15) || params["resolution"] != "720p" || params["aspect_ratio"] != "16:9" {
				t.Fatalf("body = %#v", body)
			}
			if tc.wantImages != nil && !reflect.DeepEqual(params["images"], tc.wantImages) {
				t.Fatalf("images = %#v", params["images"])
			}
			if len(tc.videos) > 0 && !reflect.DeepEqual(params["videos"], []any{"https://cdn.example/ref.mp4"}) {
				t.Fatalf("videos = %#v", params["videos"])
			}
			if len(tc.audios) > 0 && !reflect.DeepEqual(params["audios"], []any{"https://cdn.example/ref.wav"}) {
				t.Fatalf("audios = %#v", params["audios"])
			}
			if _, found := body["duration"]; found {
				t.Fatal("duration escaped params envelope")
			}
		})
	}
	invalid := GenerationRequest{Model: "seedance-2-0-special", Prompt: "clip", Extra: map[string]any{"idempotencyKey": "stable-task-key"}, Inputs: []MediaReference{{Kind: "image", URL: "https://app.example/temporary"}}}
	if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: invalid}); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("missing upload URL: %v", err)
	}
	invalid.Inputs = nil
	invalid.Extra = nil
	if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: invalid}); err == nil {
		t.Fatal("missing idempotency key accepted")
	}
}

func TestJuchuangVideoResponseLifecycle(t *testing.T) {
	adapter := officialPackageAdapter(t, "juchuang-video.yingce-plugin", "juchuang-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"code":200,"data":{"task_id":"task-123","status":"pending","is_final":false}}`))
	if err != nil || created.TaskID != "task-123" || created.Status != StatusPending {
		t.Fatalf("create = %+v, %v", created, err)
	}
	poll := PollContext{TaskID: "task-123"}
	spec, err := adapter.BuildPoll(context.Background(), poll)
	if err != nil || spec.Path != "/v1/tasks/task-123" {
		t.Fatalf("poll = %+v, %v", spec, err)
	}
	for _, tc := range []struct {
		name, body   string
		status       Status
		url, message string
	}{
		{"processing", `{"status":"processing","is_final":false}`, StatusPending, "", ""},
		{"review", `{"status":"needs_review","is_final":false,"output_url":"https://cdn.example/unfinished.mp4"}`, StatusPending, "", ""},
		{"not final", `{"status":"completed","is_final":false,"output_url":"https://cdn.example/unfinished.mp4"}`, StatusPending, "", ""},
		{"completed", `{"status":"completed","is_final":true,"output_url":"https://cdn.example/final.mp4","result_url":"https://cdn.example/old.mp4"}`, StatusSucceeded, "https://cdn.example/final.mp4", ""},
		{"failed string", `{"status":"failed","is_final":true,"error":"输入素材不符合模型要求"}`, StatusFailed, "", "输入素材不符合模型要求"},
		{"failed object", `{"status":"failed","is_final":true,"error":{"code":"invalid_parameter","message":"参考视频时长超出范围"}}`, StatusFailed, "", "参考视频时长超出范围"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := adapter.ParsePoll(context.Background(), poll, []byte(tc.body))
			if err != nil || state.Status != tc.status || (tc.message != "" && state.Message != tc.message) {
				t.Fatalf("state = %+v, %v", state, err)
			}
			if tc.url != "" && (state.Result == nil || len(state.Result.Videos) != 1 || state.Result.Videos[0].URL != tc.url) {
				t.Fatalf("video result = %+v", state.Result)
			}
			if tc.url == "" && state.Result != nil {
				t.Fatalf("unexpected result = %+v", state.Result)
			}
		})
	}
}
