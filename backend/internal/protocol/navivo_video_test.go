package protocol

import (
	"context"
	"testing"
)

func TestOfficialNavivoVideoSeedanceCreateBody(t *testing.T) {
	adapter := officialPackageAdapter(t, "navivo-video.yingce-plugin", "navivo-video")
	metadata := adapter.Metadata()
	if !metadata.RequiresPublicMediaURLs || !metadata.DetachedPolling {
		t.Fatalf("navivo metadata = %#v", metadata)
	}
	request := GenerationRequest{
		Model: "dola-seedance-20-fast-text-video", Prompt: "海浪拍打礁石", Duration: 15, AspectRatio: "16:9", Resolution: "720p",
		Images: []MediaReference{
			{URL: "https://cdn.example/second.png", Order: 2},
			{URL: "https://cdn.example/first.png", Order: 1},
		},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/v1/videos" || create.ContentType != "application/json" || len(create.Files) != 0 {
		t.Fatalf("navivo create = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["aspect_ratio"] != "16:9" {
		t.Fatalf("navivo create body = %#v", body)
	}
	for _, key := range []string{"duration", "seconds", "size", "resolution", "videos", "audios"} {
		if _, exists := body[key]; exists {
			t.Fatalf("navivo seedance body should omit %s: %#v", key, body)
		}
	}
	images, _ := body["images"].([]any)
	if len(images) != 2 || images[0] != "https://cdn.example/first.png" || images[1] != "https://cdn.example/second.png" {
		t.Fatalf("navivo seedance images = %#v", body["images"])
	}
}

func TestOfficialNavivoVideoSD933CreateBody(t *testing.T) {
	adapter := officialPackageAdapter(t, "navivo-video.yingce-plugin", "navivo-video")
	for _, model := range []string{"sd 933", "SD933"} {
		request := GenerationRequest{
			Model: model, Prompt: "参考图片、视频动作和音频节奏", Duration: 5,
			Images: []MediaReference{{URL: "https://cdn.example/image.jpg"}},
			Videos: []MediaReference{{URL: "https://cdn.example/video.mp4"}},
			Audios: []MediaReference{{URL: "https://cdn.example/audio.mp3"}},
		}
		create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
		if err != nil {
			t.Fatalf("%s: %v", model, err)
		}
		body := manifestTestBody(t, create)
		if body["model"] != model || body["duration"] != float64(5) {
			t.Fatalf("%s body = %#v", model, body)
		}
		if _, exists := body["aspect_ratio"]; exists {
			t.Fatalf("%s should omit empty aspect_ratio: %#v", model, body)
		}
		for key, want := range map[string]string{"images": request.Images[0].URL, "videos": request.Videos[0].URL, "audios": request.Audios[0].URL} {
			items, _ := body[key].([]any)
			if len(items) != 1 {
				t.Fatalf("%s %s = %#v", model, key, body[key])
			}
			item, _ := items[0].(map[string]any)
			if len(item) != 1 || item["url"] != want {
				t.Fatalf("%s %s = %#v", model, key, body[key])
			}
		}
	}
}

func TestOfficialNavivoVideoOmitsDefaultsAndValidates(t *testing.T) {
	adapter := officialPackageAdapter(t, "navivo-video.yingce-plugin", "navivo-video")
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "sd 933", Prompt: "清晨湖边"}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, create)
	for _, key := range []string{"duration", "aspect_ratio", "images", "videos", "audios"} {
		if _, exists := body[key]; exists {
			t.Fatalf("navivo body should omit %s: %#v", key, body)
		}
	}
	for name, request := range map[string]GenerationRequest{
		"empty prompt":       {Model: "dola-seedance-25-text-video", Prompt: "  "},
		"aspect ratio":       {Model: "dola-seedance-25-text-video", Prompt: "p", AspectRatio: "2:1"},
		"seedance video ref": {Model: "dola-seedance-25-text-video", Prompt: "p", Videos: []MediaReference{{URL: "https://cdn.example/v.mp4"}}},
		"seedance audio ref": {Model: "dola-seedance-20-fast-text-video", Prompt: "p", Audios: []MediaReference{{URL: "https://cdn.example/a.mp3"}}},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestOfficialNavivoVideoLifecycle(t *testing.T) {
	adapter := officialPackageAdapter(t, "navivo-video.yingce-plugin", "navivo-video")
	request := GenerationRequest{Model: "dola-seedance-20-fast-text-video", Prompt: "p"}
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task_example123","status":"queued"}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task_example123" || created.Status != StatusPending || created.Result != nil {
		t.Fatalf("navivo create result = %#v", created)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/videos/task_example123" {
		t.Fatalf("navivo poll = %#v", poll)
	}
	pollContext := PollContext{Request: request, TaskID: created.TaskID}
	running, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_example123","status":"in_progress"}`))
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != StatusProcessing || running.Stage != "上游生成中" {
		t.Fatalf("navivo running = %#v", running)
	}
	const directURL = "https://cdn.example.com/video.mp4"
	for _, payload := range []string{
		`{"id":"task_example123","status":"completed","url":"` + directURL + `"}`,
		`{"id":"task_example123","status":"SUCCESS","video_url":"` + directURL + `"}`,
		`{"id":"task_example123","status":"completed","metadata":{"url":"` + directURL + `"}}`,
	} {
		done, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if done.Status != StatusSucceeded || done.Result == nil || len(done.Result.Videos) != 1 || done.Result.Videos[0].URL != directURL {
			t.Fatalf("navivo completed %s = %#v", payload, done)
		}
	}
	noURL, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_example123","status":"completed"}`))
	if err != nil {
		t.Fatal(err)
	}
	if noURL.Status != StatusSucceeded || noURL.Result != nil {
		t.Fatalf("navivo completed without url = %#v", noURL)
	}
	failed, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_example123","status":"failed","error":{"message":"参考图不可访问"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Message != "参考图不可访问" {
		t.Fatalf("navivo failed = %#v", failed)
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("navivo adapter does not expose result download")
	}
	result, err := resultAdapter.BuildResult(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task_example123/content" {
		t.Fatalf("navivo result = %#v", result)
	}
}
