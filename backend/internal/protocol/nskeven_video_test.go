package protocol

import (
	"context"
	"strings"
	"testing"
)

func nskevenVideoAdapter(t *testing.T) Adapter {
	t.Helper()
	return officialPackageAdapter(t, "nskeven-video.yingce-plugin", "nskeven-video")
}

func nskevenCreateBody(t *testing.T, request GenerationRequest) map[string]any {
	t.Helper()
	create, err := nskevenVideoAdapter(t).BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/v1/video/generations" || create.ContentType != "application/json" || len(create.Files) != 0 {
		t.Fatalf("nskeven create = %#v", create)
	}
	return manifestTestBody(t, create)
}

func nskevenStrings(t *testing.T, value any) []string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("expected string array, got %#v", value)
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok {
			t.Fatalf("expected string item, got %#v", item)
		}
		result = append(result, text)
	}
	return result
}

func TestOfficialNskevenVideoMetadata(t *testing.T) {
	metadata := nskevenVideoAdapter(t).Metadata()
	if !metadata.RequiresPublicMediaURLs || !metadata.DetachedPolling {
		t.Fatalf("nskeven metadata = %#v, want public media URLs and detached polling", metadata)
	}
}

func TestOfficialNskevenVideoFlatBody(t *testing.T) {
	body := nskevenCreateBody(t, GenerationRequest{
		Model: "even-sd2.5-A", Prompt: "按 @图片1 和 @图片2 的构图，参考 @视频1 运镜，配 @音频1", Duration: 8, AspectRatio: "9:16", Resolution: "720P", GenerateAudio: true,
		Images: []MediaReference{
			{URL: "https://cdn.example/ref-2.png", Role: "reference_image", Order: 3},
			{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1},
			{URL: "https://cdn.example/ref-1.png", Order: 2},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/cam.mp4", Role: "reference_video"}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Role: "reference_audio"}},
	})
	if body["model"] != "even-sd2.5-A" || body["seconds"] != "8" || body["resolution"] != "720p" || body["aspect_ratio"] != "9:16" || body["generate_audio"] != true {
		t.Fatalf("nskeven flat body = %#v", body)
	}
	if body["prompt"] != "按 @图1 和 @图2 的构图，参考 @视频1 运镜，配 @音频1" {
		t.Fatalf("nskeven A-series prompt = %#v", body["prompt"])
	}
	images := nskevenStrings(t, body["images"])
	if len(images) != 2 || images[0] != "https://cdn.example/ref-1.png" || images[1] != "https://cdn.example/ref-2.png" {
		t.Fatalf("nskeven reference images = %#v", images)
	}
	if body["first_frame_url"] != "https://cdn.example/first.png" {
		t.Fatalf("nskeven first frame = %#v", body["first_frame_url"])
	}
	if _, exists := body["last_frame_url"]; exists {
		t.Fatalf("nskeven body should omit last_frame_url: %#v", body)
	}
	videos := nskevenStrings(t, body["videos"])
	audios := nskevenStrings(t, body["audios"])
	if len(videos) != 1 || videos[0] != "https://cdn.example/cam.mp4" || len(audios) != 1 || audios[0] != "https://cdn.example/voice.mp3" {
		t.Fatalf("nskeven flat media = videos:%#v audios:%#v", videos, audios)
	}
	if _, exists := body["metadata"]; exists {
		t.Fatalf("nskeven flat body should omit metadata: %#v", body)
	}
}

func TestOfficialNskevenVideoKeepsPromptForOtherModels(t *testing.T) {
	body := nskevenCreateBody(t, GenerationRequest{
		Model: "video-ultra-720p", Prompt: "@图片1 从清晨过渡到黄昏", Duration: 5,
		Images: []MediaReference{
			{URL: "https://cdn.example/last.png", Role: "last_frame", Order: 2},
			{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1},
		},
	})
	if body["prompt"] != "@图片1 从清晨过渡到黄昏" {
		t.Fatalf("nskeven non-A prompt should be unchanged: %#v", body["prompt"])
	}
	if body["first_frame_url"] != "https://cdn.example/first.png" || body["last_frame_url"] != "https://cdn.example/last.png" {
		t.Fatalf("nskeven keyframes = %#v", body)
	}
	if _, exists := body["images"]; exists {
		t.Fatalf("nskeven keyframe-only body should omit images: %#v", body)
	}
}

func TestOfficialNskevenVideoOmitsDefaults(t *testing.T) {
	body := nskevenCreateBody(t, GenerationRequest{Model: "video-ultra-720p", Prompt: "清晨湖边"})
	if len(body) != 2 || body["model"] != "video-ultra-720p" || body["prompt"] != "清晨湖边" {
		t.Fatalf("nskeven minimal body = %#v, want only model and prompt", body)
	}
}

func TestOfficialNskevenVideoDoubaoBody(t *testing.T) {
	body := nskevenCreateBody(t, GenerationRequest{
		Model: "doubao-seedance-2-0-fast-260128", Prompt: "参考图片中的人物生成街头镜头", Duration: 8, AspectRatio: "21:9", Resolution: "720p", Watermark: true,
		Images: []MediaReference{
			{URL: "https://cdn.example/ref-b.png", Order: 2},
			{URL: "https://cdn.example/ref-a.png", Role: "reference_image", Order: 1},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/ref.mp4"}},
		Audios: []MediaReference{{URL: "https://cdn.example/ref.mp3"}},
	})
	for _, key := range []string{"resolution", "aspect_ratio", "generate_audio", "images", "videos", "audios", "first_frame_url", "last_frame_url"} {
		if _, exists := body[key]; exists {
			t.Fatalf("nskeven doubao body should omit top-level %s: %#v", key, body)
		}
	}
	if body["seconds"] != "8" {
		t.Fatalf("nskeven doubao seconds = %#v", body["seconds"])
	}
	metadata, _ := body["metadata"].(map[string]any)
	if metadata["resolution"] != "720p" || metadata["ratio"] != "21:9" || metadata["generate_audio"] != false || metadata["watermark"] != true {
		t.Fatalf("nskeven doubao metadata = %#v", metadata)
	}
	content, _ := metadata["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("nskeven doubao content = %#v", metadata["content"])
	}
	want := []struct{ kind, url, role string }{
		{"image_url", "https://cdn.example/ref-a.png", "reference_image"},
		{"image_url", "https://cdn.example/ref-b.png", "reference_image"},
		{"video_url", "https://cdn.example/ref.mp4", "reference_video"},
		{"audio_url", "https://cdn.example/ref.mp3", "reference_audio"},
	}
	for index, expected := range want {
		item, _ := content[index].(map[string]any)
		media, _ := item[expected.kind].(map[string]any)
		if item["type"] != expected.kind || media["url"] != expected.url || item["role"] != expected.role {
			t.Fatalf("nskeven doubao content[%d] = %#v, want %#v", index, item, expected)
		}
	}
}

func TestOfficialNskevenVideoDoubaoKeyframes(t *testing.T) {
	body := nskevenCreateBody(t, GenerationRequest{
		Model: "doubao-seedance-2-5-260628", Prompt: "过渡", Resolution: "1080p", GenerateAudio: true,
		Images: []MediaReference{
			{URL: "https://cdn.example/last.png", Role: "last_frame", Order: 2},
			{URL: "https://cdn.example/first.png", Role: "first_frame", Order: 1},
		},
	})
	metadata, _ := body["metadata"].(map[string]any)
	content, _ := metadata["content"].([]any)
	first, _ := content[0].(map[string]any)
	last, _ := content[1].(map[string]any)
	if len(content) != 2 || first["role"] != "first_frame" || last["role"] != "last_frame" || metadata["generate_audio"] != true {
		t.Fatalf("nskeven doubao keyframes = %#v", metadata)
	}
	if _, exists := metadata["ratio"]; exists {
		t.Fatalf("nskeven doubao should omit empty ratio: %#v", metadata)
	}
	if _, exists := metadata["watermark"]; exists {
		t.Fatalf("nskeven doubao should omit false watermark: %#v", metadata)
	}
}

func TestOfficialNskevenVideoFastLineBody(t *testing.T) {
	for _, model := range []string{"sd-2-E6", "sd-2.5-E1", "wan-3.0-1080", "even-sd-2.5-30s"} {
		t.Run(model, func(t *testing.T) {
			body := nskevenCreateBody(t, GenerationRequest{
				Model: model, Prompt: "根据参考图与参考视频节奏生成同款镜头", Duration: 8, AspectRatio: "16:9", Resolution: "720p",
				Images: []MediaReference{{URL: "https://cdn.example/ref-1.jpg"}},
				Videos: []MediaReference{{URL: "https://cdn.example/ref.mp4"}},
				Audios: []MediaReference{{URL: "https://cdn.example/ref.mp3"}},
			})
			if body["resolution"] != "720p" || body["aspect_ratio"] != "16:9" || body["seconds"] != "8" {
				t.Fatalf("nskeven fast-line body = %#v", body)
			}
			images := nskevenStrings(t, body["images"])
			if len(images) != 1 || images[0] != "https://cdn.example/ref-1.jpg" {
				t.Fatalf("nskeven fast-line images = %#v", images)
			}
			for _, key := range []string{"videos", "audios"} {
				if _, exists := body[key]; exists {
					t.Fatalf("nskeven fast-line body should omit top-level %s: %#v", key, body)
				}
			}
			metadata, _ := body["metadata"].(map[string]any)
			videos := nskevenStrings(t, metadata["videos"])
			audios := nskevenStrings(t, metadata["audios"])
			if len(metadata) != 2 || len(videos) != 1 || videos[0] != "https://cdn.example/ref.mp4" || len(audios) != 1 || audios[0] != "https://cdn.example/ref.mp3" {
				t.Fatalf("nskeven fast-line metadata = %#v", metadata)
			}
		})
	}
	imageOnly := nskevenCreateBody(t, GenerationRequest{Model: "sd-2-E4", Prompt: "p", Images: []MediaReference{{URL: "https://cdn.example/ref.jpg"}}})
	if _, exists := imageOnly["metadata"]; exists {
		t.Fatalf("nskeven fast-line image-only body should omit metadata: %#v", imageOnly)
	}
}

func TestOfficialNskevenVideoValidation(t *testing.T) {
	adapter := nskevenVideoAdapter(t)
	image := func(role string) MediaReference {
		return MediaReference{URL: "https://cdn.example/" + role + ".png", Role: role}
	}
	images := func(count int) []MediaReference {
		result := make([]MediaReference, count)
		for index := range result {
			result[index] = image("reference_image")
		}
		return result
	}
	for name, test := range map[string]struct {
		request GenerationRequest
		message string
	}{
		"empty prompt":                {GenerationRequest{Model: "video-ultra-720p", Prompt: "  "}, "提示词不能为空"},
		"duration too short":          {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", Duration: 3}, "4–30 秒"},
		"duration too long":           {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", Duration: 31}, "4–30 秒"},
		"aspect ratio":                {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", AspectRatio: "2:1"}, "画幅只支持"},
		"21:9 outside doubao":         {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", AspectRatio: "21:9"}, "支持 21:9"},
		"fast-line 4:3":               {GenerationRequest{Model: "sd-2-E5", Prompt: "p", AspectRatio: "4:3"}, "快线系列"},
		"resolution":                  {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", Resolution: "2k"}, "分辨率只支持"},
		"doubao without resolution":   {GenerationRequest{Model: "doubao-seedance-2-0-260128", Prompt: "p"}, "必须选择分辨率"},
		"two first frames":            {GenerationRequest{Model: "video-ultra-720p", Prompt: "p", Images: []MediaReference{image("first_frame"), image("first_frame")}}, "各最多 1 张"},
		"doubao keyframes with video": {GenerationRequest{Model: "doubao-seedance-2-0-260128", Prompt: "p", Resolution: "720p", Images: []MediaReference{image("first_frame")}, Videos: []MediaReference{{URL: "https://cdn.example/v.mp4"}}}, "使用首尾帧"},
		"doubao keyframes images":     {GenerationRequest{Model: "doubao-seedance-2-0-260128", Prompt: "p", Resolution: "720p", Images: []MediaReference{image("first_frame"), image("last_frame"), image("reference_image")}}, "使用首尾帧"},
		"fast-line too many images":   {GenerationRequest{Model: "sd-2-E6", Prompt: "p", Images: images(10)}, "最多 9 张"},
		"A-series image without @":    {GenerationRequest{Model: "even-sd2.0-A", Prompt: "p", Images: images(1)}, "@图1"},
		"A-series video without @":    {GenerationRequest{Model: "even-sd2.5-A", Prompt: "@图片1", Images: images(1), Videos: []MediaReference{{URL: "https://cdn.example/v.mp4"}}}, "@视频1"},
	} {
		_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: test.request})
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("%s: error = %v, want message containing %q", name, err, test.message)
		}
	}
	for name, request := range map[string]GenerationRequest{
		"doubao 21:9":            {Model: "doubao-seedance-2-0-260128", Prompt: "p", Resolution: "4K", AspectRatio: "21:9"},
		"A-series first frame":   {Model: "even-sd2.5-A", Prompt: "p", Images: []MediaReference{image("first_frame")}},
		"fast-line nine images":  {Model: "sd-2-E6", Prompt: "p", Images: images(9)},
		"B-series without @":     {Model: "even-sd2.5-B", Prompt: "p", Images: images(1)},
		"doubao two keyframes":   {Model: "doubao-seedance-2-0-260128", Prompt: "p", Resolution: "720p", Images: []MediaReference{image("first_frame"), image("last_frame")}},
		"duration bounds":        {Model: "video-ultra2.5-720p", Prompt: "p", Duration: 30},
		"uppercase 4K flat line": {Model: "even--seedance-2.0-4k", Prompt: "p", Resolution: "4K"},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err != nil {
			t.Fatalf("%s: unexpected validation error: %v", name, err)
		}
	}
}

func TestOfficialNskevenVideoLifecycle(t *testing.T) {
	adapter := nskevenVideoAdapter(t)
	request := GenerationRequest{Model: "even-sd2.5-A", Prompt: "p"}
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task_385412","task_id":"task_385412","object":"video","status":"queued","created_at":1790224802}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task_385412" || created.Status != StatusPending || created.Stage != "排队中" || created.Result != nil {
		t.Fatalf("nskeven create result = %#v", created)
	}
	pollContext := PollContext{Request: request, TaskID: created.TaskID}
	poll, err := adapter.BuildPoll(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/video/generations/task_385412" {
		t.Fatalf("nskeven poll = %#v", poll)
	}
	for _, test := range []struct {
		status string
		want   Status
		stage  string
	}{
		{"NOT_START", StatusPending, "排队中"},
		{"SUBMITTED", StatusPending, "排队中"},
		{"QUEUED", StatusPending, "排队中"},
		{"IN_PROGRESS", StatusProcessing, "生成中"},
		{"UNKNOWN", StatusPending, "上游状态未知，继续查询"},
	} {
		state, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"code":"success","message":"","data":{"id":883,"task_id":"task_385412","status":"`+test.status+`","progress":"100%","fail_reason":"","result_url":"","data":{"status":"SUCCEEDED"}}}`))
		if err != nil {
			t.Fatal(err)
		}
		if state.TaskID != "task_385412" || state.Status != test.want || state.Stage != test.stage || state.Result != nil {
			t.Fatalf("nskeven %s = %#v", test.status, state)
		}
	}
	done, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"code":"success","message":"","data":{"id":883,"task_id":"task_385412","status":"SUCCESS","progress":"100%","fail_reason":"残留文案","result_url":"https://newapi.nskeven.uk/v1/videos/task_385412/content"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusSucceeded || done.Result != nil || done.Message != "" {
		t.Fatalf("nskeven completed = %#v, want success that downloads through /content", done)
	}
	const reason = "生成被拒绝，内容安全审核未通过，修改提示词或素材后重试"
	failed, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"code":"success","message":"","data":{"id":883,"task_id":"task_385412","status":"FAILURE","progress":"100%","fail_reason":"`+reason+`","result_url":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Message != reason {
		t.Fatalf("nskeven failed = %#v", failed)
	}
	nested, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"code":"success","message":"","data":{"task_id":"task_385412","status":"FAILURE","fail_reason":"","data":{"error":{"message":"OutputVideoSensitiveContentDetected"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if nested.Status != StatusFailed || nested.Message != "OutputVideoSensitiveContentDetected" {
		t.Fatalf("nskeven nested failure = %#v", nested)
	}
	rejected, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"error":{"message":"No available channel for model xxx under group default (distributor)","type":"new_api_error","code":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if rejected.Status != StatusFailed || rejected.Message != "No available channel for model xxx under group default (distributor)" {
		t.Fatalf("nskeven error object = %#v", rejected)
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("nskeven adapter does not expose result download")
	}
	result, err := resultAdapter.BuildResult(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task_385412/content" || result.Headers["Accept"] != "video/mp4" || result.Auth.Type != "bearer" {
		t.Fatalf("nskeven result = %#v", result)
	}
}
