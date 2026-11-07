package protocol

import (
	"context"
	"encoding/json"
	"testing"
)

func uutokenVideoAdapter(t *testing.T) Adapter {
	t.Helper()
	return officialPackageAdapter(t, "uutoken-video.yingce-plugin", "uutoken-video")
}

func uutokenCreateBody(t *testing.T, adapter Adapter, request GenerationRequest) map[string]any {
	t.Helper()
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatalf("%s: %v", request.Model, err)
	}
	if create.Method != "POST" || create.Path != "/v1/videos" || create.ContentType != "application/json" || len(create.Files) != 0 {
		t.Fatalf("%s create = %#v", request.Model, create)
	}
	return manifestTestBody(t, create)
}

func assertBodyOmits(t *testing.T, label string, body map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, exists := body[key]; exists {
			t.Fatalf("%s should omit %s: %#v", label, key, body)
		}
	}
}

func assertStringList(t *testing.T, label string, value any, want ...string) {
	t.Helper()
	items, _ := value.([]any)
	if len(items) != len(want) {
		t.Fatalf("%s = %#v, want %v", label, value, want)
	}
	for index, item := range items {
		if item != want[index] {
			t.Fatalf("%s = %#v, want %v", label, value, want)
		}
	}
}

func TestOfficialUUTokenVideoGenericCreateBody(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	metadata := adapter.Metadata()
	if !metadata.RequiresPublicMediaURLs || !metadata.DetachedPolling {
		t.Fatalf("uutoken metadata = %#v", metadata)
	}
	request := GenerationRequest{
		Model: "seedance-2.0-g3", Prompt: "海边日落，镜头缓慢推进", Duration: 5, AspectRatio: "16:9", Resolution: "720P",
		Images: []MediaReference{
			{URL: "https://cdn.example/second.png", Order: 2},
			{URL: "https://cdn.example/first.png", Order: 1, Role: "first_frame"},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4"}},
		Audios: []MediaReference{{URL: "https://cdn.example/music.mp3"}},
	}
	body := uutokenCreateBody(t, adapter, request)
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["seconds"] != float64(5) ||
		body["aspect_ratio"] != "16:9" || body["resolution"] != "720p" {
		t.Fatalf("generic body = %#v", body)
	}
	assertStringList(t, "generic images", body["images"], "https://cdn.example/first.png", "https://cdn.example/second.png")
	assertStringList(t, "generic videos", body["videos"], "https://cdn.example/motion.mp4")
	assertStringList(t, "generic audios", body["audios"], "https://cdn.example/music.mp3")
	assertBodyOmits(t, "generic", body, "duration", "ratio", "size", "generate_audio", "first_frame", "metadata")

	hss := uutokenCreateBody(t, adapter, GenerationRequest{Model: "hss-2.0-933-渠道A", Prompt: "p", Duration: 5, AspectRatio: "9:16", Resolution: "720p"})
	if hss["ratio"] != "9:16" || hss["resolution"] != "720p" {
		t.Fatalf("hss body = %#v", hss)
	}
	assertBodyOmits(t, "hss", hss, "aspect_ratio")

	for _, model := range []string{"seadanco2.5-720-second", "dreamina-seedance-2-5"} {
		fixed := uutokenCreateBody(t, adapter, GenerationRequest{Model: model, Prompt: "p", Duration: 5, AspectRatio: "16:9", Resolution: "720p"})
		if fixed["aspect_ratio"] != "16:9" || fixed["seconds"] != float64(5) {
			t.Fatalf("%s body = %#v", model, fixed)
		}
		assertBodyOmits(t, model, fixed, "resolution")
	}

	empty := uutokenCreateBody(t, adapter, GenerationRequest{Model: "wan-3.0-prime", Prompt: "海底珊瑚"})
	assertBodyOmits(t, "empty generic", empty, "seconds", "aspect_ratio", "ratio", "resolution", "images", "videos", "audios")
}

func TestOfficialUUTokenVideoSeedanceFullFrames(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	frames := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "Seedance-2.0-full", Prompt: "从首帧自然过渡到尾帧", Duration: 5, AspectRatio: "16:9", Resolution: "1080p",
		Images: []MediaReference{
			{URL: "https://cdn.example/last.jpg", Role: "last_frame", Order: 2},
			{URL: "https://cdn.example/first.jpg", Role: "first_frame", Order: 1},
		},
	})
	if frames["first_frame"] != "https://cdn.example/first.jpg" || frames["last_frame"] != "https://cdn.example/last.jpg" ||
		frames["ratio"] != "16:9" || frames["resolution"] != "1080p" || frames["seconds"] != float64(5) {
		t.Fatalf("full frames body = %#v", frames)
	}
	assertBodyOmits(t, "full frames", frames, "images", "videos", "audios", "aspect_ratio")

	refs := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "Seedance-2.0-full", Prompt: "参考主体", Duration: 5, AspectRatio: "16:9", Resolution: "1080p",
		Images: []MediaReference{{URL: "https://cdn.example/person.jpg", Role: "reference_image"}},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4"}},
	})
	assertStringList(t, "full images", refs["images"], "https://cdn.example/person.jpg")
	assertStringList(t, "full videos", refs["videos"], "https://cdn.example/motion.mp4")
	assertBodyOmits(t, "full refs", refs, "first_frame", "last_frame")

	_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "Seedance-2.0-full", Prompt: "p",
		Images: []MediaReference{{URL: "https://cdn.example/first.jpg", Role: "first_frame"}},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4"}},
	}})
	if err == nil {
		t.Fatal("full frame mode with reference video should be rejected")
	}
}

func TestOfficialUUTokenVideoGFPayload(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	decodePayload := func(body map[string]any) map[string]any {
		t.Helper()
		metadata, _ := body["metadata"].(map[string]any)
		raw, _ := metadata["payload"].(string)
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("gf payload %q: %v", raw, err)
		}
		return payload
	}
	refs := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "sd2.5-gf-720p", Prompt: "保持参考主体一致", Duration: 5, AspectRatio: "16:9", Resolution: "720p",
		Images: []MediaReference{{URL: "https://cdn.example/person.jpg"}},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4"}},
		Audios: []MediaReference{{URL: "https://cdn.example/music.mp3"}},
	})
	if refs["duration"] != float64(5) || refs["model"] != "sd2.5-gf-720p" {
		t.Fatalf("gf body = %#v", refs)
	}
	assertBodyOmits(t, "gf", refs, "seconds", "resolution", "aspect_ratio", "images", "videos", "audios")
	payload := decodePayload(refs)
	if payload["aspectRatio"] != "16:9" || payload["mode"] != "references" {
		t.Fatalf("gf refs payload = %#v", payload)
	}
	assertStringList(t, "gf imageUrls", payload["imageUrls"], "https://cdn.example/person.jpg")
	assertStringList(t, "gf videoUrls", payload["videoUrls"], "https://cdn.example/motion.mp4")
	assertStringList(t, "gf audioUrls", payload["audioUrls"], "https://cdn.example/music.mp3")
	assertBodyOmits(t, "gf refs payload", payload, "firstFrameUrl", "lastFrameUrl")

	frames := decodePayload(uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "sd2.5-gf-480p", Prompt: "从首帧过渡到尾帧", Duration: 5, AspectRatio: "9:16",
		Images: []MediaReference{
			{URL: "https://cdn.example/first.jpg", Role: "first_frame"},
			{URL: "https://cdn.example/last.jpg", Role: "last_frame"},
		},
	}))
	if frames["mode"] != "frames" || frames["firstFrameUrl"] != "https://cdn.example/first.jpg" || frames["lastFrameUrl"] != "https://cdn.example/last.jpg" {
		t.Fatalf("gf frames payload = %#v", frames)
	}
	assertBodyOmits(t, "gf frames payload", frames, "imageUrls", "videoUrls", "audioUrls")
}

func TestOfficialUUTokenVideoKlingBody(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	v3 := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "kling-v3", Prompt: "Continue the motion", Duration: 10, AspectRatio: "16:9", Resolution: "720p",
		Images: []MediaReference{
			{URL: "https://cdn.example/last.jpg", Role: "last_frame", Order: 2},
			{URL: "https://cdn.example/first.jpg", Role: "first_frame", Order: 1},
		},
	})
	if v3["start_frame"] != "https://cdn.example/first.jpg" || v3["end_frame"] != "https://cdn.example/last.jpg" ||
		v3["resolution"] != "720P" || v3["seconds"] != float64(10) || v3["aspect_ratio"] != "16:9" {
		t.Fatalf("kling-v3 body = %#v", v3)
	}
	assertBodyOmits(t, "kling-v3", v3, "images", "reference_images", "input_video", "size")

	single := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "kling-v3", Prompt: "p", Images: []MediaReference{{URL: "https://cdn.example/only.jpg", Role: "reference_image"}},
	})
	if single["start_frame"] != "https://cdn.example/only.jpg" {
		t.Fatalf("kling-v3 single image body = %#v", single)
	}
	assertBodyOmits(t, "kling-v3 single", single, "end_frame", "reference_images", "resolution", "seconds")

	o3 := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "kling-o3", Prompt: "Use the source video motion", Duration: 8, Resolution: "1080p",
		Images: []MediaReference{{URL: "https://cdn.example/subject.png"}},
		Videos: []MediaReference{{URL: "https://cdn.example/source.mp4"}},
	})
	if o3["input_video"] != "https://cdn.example/source.mp4" || o3["resolution"] != "1080P" {
		t.Fatalf("kling-o3 body = %#v", o3)
	}
	assertStringList(t, "kling-o3 reference_images", o3["reference_images"], "https://cdn.example/subject.png")
	assertBodyOmits(t, "kling-o3", o3, "start_frame", "end_frame", "images", "videos")

	for name, request := range map[string]GenerationRequest{
		"v3 audio":       {Model: "kling-v3", Prompt: "p", Audios: []MediaReference{{URL: "https://cdn.example/a.mp3"}}},
		"v3 multi image": {Model: "kling-v3", Prompt: "p", Images: []MediaReference{{URL: "https://cdn.example/1.png"}, {URL: "https://cdn.example/2.png"}}},
		"v3 video":       {Model: "kling-v3", Prompt: "p", Videos: []MediaReference{{URL: "https://cdn.example/v.mp4"}}},
		"o3 frame mix": {Model: "kling-o3", Prompt: "p", Images: []MediaReference{
			{URL: "https://cdn.example/first.png", Role: "first_frame"}, {URL: "https://cdn.example/ref.png"},
		}},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestOfficialUUTokenVideoSoraAndOmniBody(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	sora := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "sora-v3-933-pro", Prompt: "雨夜霓虹街道", Duration: 10, AspectRatio: "16:9",
		Images: []MediaReference{{URL: "https://cdn.example/input.jpg"}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3"}},
	})
	if sora["seconds"] != "10" || sora["aspect_ratio"] != "16:9" || sora["resolution"] != "720p" {
		t.Fatalf("sora body = %#v", sora)
	}
	assertStringList(t, "sora image_urls", sora["image_urls"], "https://cdn.example/input.jpg")
	assertStringList(t, "sora audio_urls", sora["audio_urls"], "https://cdn.example/voice.mp3")
	assertBodyOmits(t, "sora", sora, "images", "audios", "video_urls")

	omni := uutokenCreateBody(t, adapter, GenerationRequest{
		Model: "gemini-omni-flash", Prompt: "Keep the character identity", Duration: 8, AspectRatio: "9:16", Resolution: "720p",
		Images: []MediaReference{
			{URL: "https://cdn.example/element.png", Order: 2},
			{URL: "https://cdn.example/first.jpg", Role: "first_frame", Order: 1},
		},
	})
	if omni["seconds"] != float64(8) || omni["aspect_ratio"] != "9:16" {
		t.Fatalf("omni body = %#v", omni)
	}
	assertStringList(t, "omni images", omni["images"], "https://cdn.example/first.jpg")
	assertStringList(t, "omni reference_images", omni["reference_images"], "https://cdn.example/element.png")
	assertBodyOmits(t, "omni", omni, "resolution", "generate_audio", "audio", "input_video")

	for name, request := range map[string]GenerationRequest{
		"empty prompt":       {Model: "seedance-2.0-g3", Prompt: "  "},
		"aspect ratio":       {Model: "seedance-2.0-g3", Prompt: "p", AspectRatio: "2:1"},
		"resolution":         {Model: "seedance-2.0-g3", Prompt: "p", Resolution: "4k"},
		"duration":           {Model: "seedance-2.0-g3", Prompt: "p", Duration: 31},
		"last without first": {Model: "seedance-2.0-g3", Prompt: "p", Images: []MediaReference{{URL: "https://cdn.example/l.png", Role: "last_frame"}}},
		"sora duration":      {Model: "sora-v3-933-fast", Prompt: "p", Duration: 5, AspectRatio: "16:9"},
		"sora no aspect":     {Model: "sora-v3-933-fast", Prompt: "p", Duration: 15},
		"sora audio only":    {Model: "sora-v3-933-pro", Prompt: "p", Duration: 10, AspectRatio: "16:9", Audios: []MediaReference{{URL: "https://cdn.example/a.mp3"}}},
		"omni audio":         {Model: "gemini-omni-flash", Prompt: "p", Audios: []MediaReference{{URL: "https://cdn.example/a.mp3"}}},
		"omni duration":      {Model: "gemini-omni-flash", Prompt: "p", Duration: 15},
		"omni aspect":        {Model: "gemini-omni-flash", Prompt: "p", AspectRatio: "1:1"},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestOfficialUUTokenVideoLifecycle(t *testing.T) {
	adapter := uutokenVideoAdapter(t)
	request := GenerationRequest{Model: "mg-sd25-1-pro", Prompt: "p"}
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task_xxx","task_id":"task_xxx","object":"video","model":"mg-sd25-1-pro","status":"queued","progress":0}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task_xxx" || created.Status != StatusPending || created.Result != nil {
		t.Fatalf("uutoken create result = %#v", created)
	}
	pollContext := PollContext{Request: request, TaskID: created.TaskID}
	poll, err := adapter.BuildPoll(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/videos/task_xxx" {
		t.Fatalf("uutoken poll = %#v", poll)
	}
	for payload, want := range map[string]Status{
		`{"id":"task_xxx","status":"NOT_START"}`:   StatusPending,
		`{"id":"task_xxx","status":"IN_PROGRESS"}`: StatusProcessing,
		`{"id":"task_xxx","status":"running"}`:     StatusProcessing,
	} {
		result, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != want || result.Result != nil {
			t.Fatalf("uutoken poll %s = %#v", payload, result)
		}
	}
	queued, _ := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_xxx","status":"NOT_START"}`))
	running, _ := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_xxx","status":"in_progress"}`))
	if queued.Stage != "上游排队中" || running.Stage != "上游生成中" {
		t.Fatalf("uutoken stages = %q / %q", queued.Stage, running.Stage)
	}
	const directURL = "https://oss.example.com/video.mp4?signature=abc"
	for _, payload := range []string{
		`{"id":"task_xxx","status":"completed","video_url":"` + directURL + `"}`,
		`{"id":"task_xxx","status":"SUCCESS","url":"` + directURL + `"}`,
	} {
		done, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if done.Status != StatusSucceeded || done.Result == nil || len(done.Result.Videos) != 1 ||
			done.Result.Videos[0].URL != directURL || !done.Result.Videos[0].Ephemeral {
			t.Fatalf("uutoken completed %s = %#v", payload, done)
		}
	}
	noURL, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_xxx","status":"succeeded"}`))
	if err != nil {
		t.Fatal(err)
	}
	if noURL.Status != StatusSucceeded || noURL.Result != nil {
		t.Fatalf("uutoken completed without url = %#v", noURL)
	}
	for payload, want := range map[string]string{
		`{"id":"task_xxx","status":"FAILURE","fail_reason":"参考素材不可访问"}`:                  "参考素材不可访问",
		`{"id":"task_xxx","status":"failed","error":{"message":"No available channel"}}`: "No available channel",
	} {
		failed, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if failed.Status != StatusFailed || failed.Message != want {
			t.Fatalf("uutoken failed %s = %#v", payload, failed)
		}
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("uutoken adapter does not expose result download")
	}
	result, err := resultAdapter.BuildResult(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task_xxx/content" {
		t.Fatalf("uutoken result = %#v", result)
	}
}
