package protocol

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestAotianVideoCreateSendsPublicReferenceURLsAsJSON(t *testing.T) {
	adapter := officialPackageAdapter(t, "aotian-video.yingce-plugin", "aotian-video")
	metadata := adapter.Metadata()
	if !metadata.RequiresPublicMediaURLs || !metadata.DetachedPolling || metadata.IdempotentCreate {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	image := func(role, url string, order int) MediaReference {
		return MediaReference{Kind: "image", Role: role, URL: url, Order: order}
	}
	request := GenerationRequest{
		Model:       "特价seedance-2.0-720p",
		Prompt:      "民国赌场，暖黄灯光",
		Duration:    12,
		AspectRatio: "9:16",
		Resolution:  "720p",
		Operation:   "reference_to_video",
		Images: []MediaReference{
			image("reference_image", "https://canvas.example/api/public/resources/second/file?signature=b", 2),
			image("reference_image", "https://canvas.example/api/public/resources/first/file?signature=a", 1),
		},
		Videos: []MediaReference{{Kind: "video", Role: "reference_video", URL: "https://canvas.example/ref.mp4", Order: 3}},
		Audios: []MediaReference{{Kind: "audio", Role: "reference_audio", URL: "https://canvas.example/ref.wav", Order: 4}},
	}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/v1/videos" || spec.ContentType != "application/json" || len(spec.Files) != 0 {
		t.Fatalf("create spec = %+v", spec)
	}
	body := manifestTestBody(t, spec)
	want := map[string]any{
		"model":      "特价seedance-2.0-720p",
		"prompt":     "民国赌场，暖黄灯光",
		"seconds":    "12",
		"size":       "9:16",
		"resolution": "720p",
		"images": []any{
			"https://canvas.example/api/public/resources/first/file?signature=a",
			"https://canvas.example/api/public/resources/second/file?signature=b",
		},
		"reference_videos": []any{"https://canvas.example/ref.mp4"},
		"reference_audios": []any{"https://canvas.example/ref.wav"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body = %#v\nwant %#v", body, want)
	}

	textOnly, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "sd4-seedance2.5-720p", Prompt: "海边日落"}})
	if err != nil {
		t.Fatal(err)
	}
	if body := manifestTestBody(t, textOnly); !reflect.DeepEqual(body, map[string]any{"model": "sd4-seedance2.5-720p", "prompt": "海边日落"}) {
		t.Fatalf("text-only body = %#v", body)
	}
}

func TestAotianVideoRejectsNonPublicReferencesBeforeSubmit(t *testing.T) {
	adapter := officialPackageAdapter(t, "aotian-video.yingce-plugin", "aotian-video")
	for _, tc := range []struct {
		name    string
		request GenerationRequest
		want    string
	}{
		{name: "http image", request: GenerationRequest{Model: "m", Prompt: "clip", Images: []MediaReference{{Kind: "image", URL: "http://canvas.example/ref.png"}}}, want: "HTTPS"},
		{name: "data image", request: GenerationRequest{Model: "m", Prompt: "clip", Images: []MediaReference{{Kind: "image", DataURL: "data:image/png;base64,AA=="}}}, want: "HTTPS"},
		{name: "http video", request: GenerationRequest{Model: "m", Prompt: "clip", Videos: []MediaReference{{Kind: "video", URL: "http://canvas.example/ref.mp4"}}}, want: "HTTPS"},
		{name: "blank prompt", request: GenerationRequest{Model: "m", Prompt: "  "}, want: "视频描述"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: tc.request})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestAotianVideoResponseLifecycle(t *testing.T) {
	adapter := officialPackageAdapter(t, "aotian-video.yingce-plugin", "aotian-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task_abc","object":"video","status":"queued","progress":0}`))
	if err != nil || created.TaskID != "task_abc" || created.Status != StatusPending {
		t.Fatalf("create = %+v, %v", created, err)
	}
	poll := PollContext{TaskID: "task_abc"}
	spec, err := adapter.BuildPoll(context.Background(), poll)
	if err != nil || spec.Method != "GET" || spec.Path != "/v1/videos/task_abc" {
		t.Fatalf("poll = %+v, %v", spec, err)
	}
	for _, tc := range []struct {
		name, body   string
		status       Status
		url, message string
	}{
		{name: "queued", body: `{"id":"task_abc","status":"queued","progress":0}`, status: StatusPending},
		{name: "in progress", body: `{"id":"task_abc","status":"in_progress","progress":40}`, status: StatusProcessing},
		{name: "archiving", body: `{"id":"task_abc","status":"processing","progress":100,"url":"https://video.example/partial.mp4"}`, status: StatusProcessing},
		{name: "completed", body: `{"id":"task_abc","status":"completed","progress":100,"url":"https://video.example/final.mp4","video_url":"https://video.example/final.mp4","metadata":{"url":"https://video.example/final.mp4"}}`, status: StatusSucceeded, url: "https://video.example/final.mp4"},
		{name: "completed metadata only", body: `{"id":"task_abc","status":"completed","metadata":{"url":"https://video.example/meta.mp4"}}`, status: StatusSucceeded, url: "https://video.example/meta.mp4"},
		{name: "failed", body: `{"id":"task_abc","status":"failed","progress":100,"error":{"message":"参考图无法下载","code":"video_generation_failed"}}`, status: StatusFailed, message: "参考图无法下载"},
		{name: "relay failed", body: `{"id":"task_abc","status":"failed","fail_reason":"task failed"}`, status: StatusFailed, message: "task failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, err := adapter.ParsePoll(context.Background(), poll, []byte(tc.body))
			if err != nil || state.Status != tc.status {
				t.Fatalf("state = %+v, %v", state, err)
			}
			if tc.message != "" && state.Message != tc.message {
				t.Fatalf("failure message = %q, want %q", state.Message, tc.message)
			}
			if tc.url != "" && (state.Result == nil || len(state.Result.Videos) != 1 || state.Result.Videos[0].URL != tc.url) {
				t.Fatalf("video result = %+v", state.Result)
			}
			if tc.url == "" && state.Result != nil && len(state.Result.Videos) > 0 {
				t.Fatalf("unfinished task returned videos: %+v", state.Result)
			}
		})
	}
}
