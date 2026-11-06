package protocol

import (
	"context"
	"testing"
)

func TestOfficialNineYAPIVideoCreateBody(t *testing.T) {
	adapter := officialPackageAdapter(t, "nineyapi-video.yingce-plugin", "nineyapi-video")
	request := GenerationRequest{
		Model: "【满血】SD-2.5-720p", Prompt: "参考@图1中的主体", Duration: 5, AspectRatio: "16:9", Resolution: "720P", GenerateAudio: true,
		Images: []MediaReference{
			{URL: "https://cdn.example/second.png", Order: 2},
			{URL: "https://cdn.example/first.png", Role: "reference_image", Order: 1},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4", Role: "reference_video"}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Role: "reference_audio"}},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/v1/videos" || create.ContentType != "application/json" || len(create.Files) != 0 {
		t.Fatalf("9Y create = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["model"] != request.Model || body["seconds"] != float64(5) || body["resolution"] != "720p" || body["aspect_ratio"] != "16:9" || body["generate_audio"] != true {
		t.Fatalf("9Y create body = %#v", body)
	}
	images, _ := body["reference_images"].([]any)
	if len(images) != 2 {
		t.Fatalf("9Y reference_images = %#v", body["reference_images"])
	}
	first, _ := images[0].(map[string]any)
	second, _ := images[1].(map[string]any)
	if first["url"] != "https://cdn.example/first.png" || first["role"] != "reference_image" || second["url"] != "https://cdn.example/second.png" || second["role"] != "reference_image" {
		t.Fatalf("9Y reference_images order/role = %#v", images)
	}
	videos, _ := body["reference_videos"].([]any)
	audios, _ := body["reference_audios"].([]any)
	video, _ := videos[0].(map[string]any)
	audio, _ := audios[0].(map[string]any)
	if len(video) != 1 || video["url"] != request.Videos[0].URL || len(audio) != 1 || audio["url"] != request.Audios[0].URL {
		t.Fatalf("9Y media = videos:%#v audios:%#v", videos, audios)
	}
}

func TestOfficialNineYAPIVideoOmitsDefaults(t *testing.T) {
	adapter := officialPackageAdapter(t, "nineyapi-video.yingce-plugin", "nineyapi-video")
	request := GenerationRequest{Model: "wan3.0-特惠版720p", Prompt: "清晨湖边"}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, create)
	for _, key := range []string{"seconds", "resolution", "aspect_ratio", "generate_audio", "reference_images", "reference_videos", "reference_audios"} {
		if _, exists := body[key]; exists {
			t.Fatalf("9Y body should omit %s: %#v", key, body)
		}
	}
}

func TestOfficialNineYAPIVideoKeyframesAndValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "nineyapi-video.yingce-plugin", "nineyapi-video")
	keyframes := GenerationRequest{
		Model: "【官转】sd2.0-fast", Prompt: "过渡", Duration: 4, AspectRatio: "adaptive",
		Images: []MediaReference{{URL: "https://cdn.example/last.png", Role: "last_frame", Order: 2}, {URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1}},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: keyframes})
	if err != nil {
		t.Fatal(err)
	}
	images, _ := manifestTestBody(t, create)["reference_images"].([]any)
	first, _ := images[0].(map[string]any)
	last, _ := images[1].(map[string]any)
	if first["role"] != "first_frame" || last["role"] != "last_frame" {
		t.Fatalf("9Y keyframes = %#v", images)
	}
	for name, request := range map[string]GenerationRequest{
		"duration too short": {Model: "m", Prompt: "p", Duration: 3},
		"duration too long":  {Model: "m", Prompt: "p", Duration: 31},
		"aspect ratio":       {Model: "m", Prompt: "p", AspectRatio: "2:1"},
		"resolution":         {Model: "m", Prompt: "p", Resolution: "4k"},
		"last frame only":    {Model: "m", Prompt: "p", Images: []MediaReference{{URL: "https://cdn.example/last.png", Role: "last_frame"}}},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestOfficialNineYAPIVideoLifecycle(t *testing.T) {
	adapter := officialPackageAdapter(t, "nineyapi-video.yingce-plugin", "nineyapi-video")
	request := GenerationRequest{Model: "【官转】Minimax-H3", Prompt: "p"}
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task-9y-1","status":"queued"}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task-9y-1" || created.Status != StatusPending {
		t.Fatalf("9Y create result = %#v", created)
	}
	poll, err := adapter.BuildPoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/videos/task-9y-1" {
		t.Fatalf("9Y poll = %#v", poll)
	}
	running, err := adapter.ParsePoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID}, []byte(`{"id":"task-9y-1","status":"in_progress","progress":40}`))
	if err != nil {
		t.Fatal(err)
	}
	if running.Status != StatusProcessing {
		t.Fatalf("9Y running = %#v", running)
	}
	const directURL = "https://cdn.example/result.mp4?expires=1"
	done, err := adapter.ParsePoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID}, []byte(`{"id":"task-9y-1","status":"completed","video_url":"`+directURL+`","result_url":"`+directURL+`","expires_at":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusSucceeded || done.Result == nil || len(done.Result.Videos) != 1 || done.Result.Videos[0].URL != directURL {
		t.Fatalf("9Y completed = %#v", done)
	}
	failed, err := adapter.ParsePoll(context.Background(), PollContext{Request: request, TaskID: created.TaskID}, []byte(`{"id":"task-9y-1","status":"failed","error":{"message":"参考图不可访问"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Message != "参考图不可访问" {
		t.Fatalf("9Y failed = %#v", failed)
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("9Y adapter does not expose result download")
	}
	result, err := resultAdapter.BuildResult(context.Background(), PollContext{Request: request, TaskID: created.TaskID})
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task-9y-1/content" {
		t.Fatalf("9Y result = %#v", result)
	}
}
