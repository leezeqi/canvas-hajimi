package protocol

import (
	"context"
	"strings"
	"testing"
)

func xianfengAdapter(t *testing.T) Adapter {
	t.Helper()
	return officialPackageAdapter(t, "xianfeng-video.yingce-plugin", "xianfeng-video")
}

func xianfengClip(url string, durationMs int64, order int) MediaReference {
	return MediaReference{URL: url, Order: order, Metadata: map[string]any{"durationMs": durationMs}}
}

func TestOfficialXianfengVideoCreateBody(t *testing.T) {
	adapter := xianfengAdapter(t)
	metadata := adapter.Metadata()
	if !metadata.RequiresPublicMediaURLs || !metadata.DetachedPolling || metadata.IdempotentCreate {
		t.Fatalf("xianfeng metadata = %#v", metadata)
	}
	request := GenerationRequest{
		Model: "JD2.5满参", Prompt: "以图片1为场景，参考视频1的运镜，配合音频1的节奏", Duration: 10, AspectRatio: " Adaptive ", Resolution: "720P",
		Images: []MediaReference{
			{URL: "https://cdn.example/second.png", Order: 2, Role: "last_frame"},
			{URL: "https://cdn.example/first.png", Order: 1, Role: "first_frame"},
		},
		Videos: []MediaReference{xianfengClip("https://cdn.example/clip.mp4", 5400, 0)},
		Audios: []MediaReference{xianfengClip("https://cdn.example/beat.wav", 3000, 0)},
	}
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/v1/videos" || create.ContentType != "application/json" || len(create.Files) != 0 {
		t.Fatalf("xianfeng create = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["seconds"] != float64(10) || body["aspect_ratio"] != "auto" || body["resolution"] != "720p" {
		t.Fatalf("xianfeng create body = %#v", body)
	}
	for _, key := range []string{"duration", "ratio", "size", "n", "files", "generate_audio", "media"} {
		if _, exists := body[key]; exists {
			t.Fatalf("xianfeng body should omit %s: %#v", key, body)
		}
	}
	images, _ := body["images"].([]any)
	if len(images) != 2 {
		t.Fatalf("xianfeng images = %#v", body["images"])
	}
	for index, want := range []string{"https://cdn.example/first.png", "https://cdn.example/second.png"} {
		item, _ := images[index].(map[string]any)
		if len(item) != 1 || item["url"] != want {
			t.Fatalf("xianfeng image %d = %#v", index, images[index])
		}
	}
	for key, want := range map[string]struct {
		url     string
		seconds float64
	}{"videos": {"https://cdn.example/clip.mp4", 6}, "audios": {"https://cdn.example/beat.wav", 3}} {
		items, _ := body[key].([]any)
		if len(items) != 1 {
			t.Fatalf("xianfeng %s = %#v", key, body[key])
		}
		item, _ := items[0].(map[string]any)
		if len(item) != 2 || item["url"] != want.url || item["seconds"] != want.seconds {
			t.Fatalf("xianfeng %s item = %#v, want url %s seconds %v", key, item, want.url, want.seconds)
		}
	}
}

func TestOfficialXianfengVideoOmitsDefaults(t *testing.T) {
	adapter := xianfengAdapter(t)
	create, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "seedance-2.5", Prompt: "清晨湖边，镜头缓慢推进"}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, create)
	for _, key := range []string{"seconds", "aspect_ratio", "images", "videos", "audios"} {
		if _, exists := body[key]; exists {
			t.Fatalf("xianfeng body should omit %s: %#v", key, body)
		}
	}
	if body["model"] != "seedance-2.5" || body["resolution"] != "720p" {
		t.Fatalf("xianfeng default body = %#v", body)
	}
}

func TestOfficialXianfengVideoValidations(t *testing.T) {
	adapter := xianfengAdapter(t)
	images := func(count int) []MediaReference {
		result := make([]MediaReference, count)
		for index := range result {
			result[index] = MediaReference{URL: "https://cdn.example/image.png", Order: index + 1}
		}
		return result
	}
	clips := func(count int) []MediaReference {
		result := make([]MediaReference, count)
		for index := range result {
			result[index] = xianfengClip("https://cdn.example/clip.mp4", 3000, index+1)
		}
		return result
	}
	for name, test := range map[string]struct {
		request GenerationRequest
		message string
	}{
		"empty prompt":          {GenerationRequest{Model: "seedance-2.5", Prompt: "  "}, "视频描述不能为空"},
		"prompt too long":       {GenerationRequest{Model: "seedance-2.5", Prompt: strings.Repeat("镜", 45001)}, "最多 45000 字"},
		"wan3 model":            {GenerationRequest{Model: "wan3（班车）", Prompt: "p"}, "wan3（班车）需要先上传素材"},
		"aspect ratio":          {GenerationRequest{Model: "seedance-2.5", Prompt: "p", AspectRatio: "2:1"}, "画幅只支持"},
		"resolution":            {GenerationRequest{Model: "JD2.5满参", Prompt: "p", Resolution: "1080p"}, "只支持 720p"},
		"too many images":       {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Images: images(31)}, "最多 30 张参考图"},
		"too many videos":       {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Videos: clips(11)}, "最多 30 张参考图"},
		"too many references":   {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Images: images(25), Videos: clips(6)}, "最多 30 张参考图"},
		"duration too short":    {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Duration: 3}, "时长需为 4–30 秒"},
		"duration too long":     {GenerationRequest{Model: "JD2.5满参", Prompt: "p", Duration: 31}, "时长需为 4–30 秒"},
		"short with clip":       {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Duration: 4, Videos: clips(1)}, "时长需为 4–30 秒"},
		"short with 10 images":  {GenerationRequest{Model: "seedance-2.5", Prompt: "p", Duration: 4, Images: images(10)}, "时长需为 4–30 秒"},
		"clip without duration": {GenerationRequest{Model: "JD2.5满参", Prompt: "p", Videos: []MediaReference{{URL: "https://cdn.example/external.mp4"}}}, "缺少时长信息"},
		"clip too short":        {GenerationRequest{Model: "JD2.5满参", Prompt: "p", Audios: []MediaReference{xianfengClip("https://cdn.example/a.mp3", 1500, 1)}}, "每条需为 2–30 秒"},
		"clip too long":         {GenerationRequest{Model: "JD2.5满参", Prompt: "p", Videos: []MediaReference{xianfengClip("https://cdn.example/v.mp4", 30500, 1)}}, "每条需为 2–30 秒"},
	} {
		_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: test.request})
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("%s: error = %v, want message containing %q", name, err, test.message)
		}
	}
	for name, request := range map[string]GenerationRequest{
		"4s with 9 images": {Model: "seedance-2.5", Prompt: "p", Duration: 4, Images: images(9), AspectRatio: "21:9"},
		"5s with clips":    {Model: "JD2.5满参", Prompt: "p", Duration: 5, Videos: clips(10), Audios: clips(10), Images: images(10)},
		"30s boundary":     {Model: "JD2.5满参", Prompt: "p", Duration: 30, Videos: []MediaReference{xianfengClip("https://cdn.example/v.mp4", 30000, 1)}},
	} {
		if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err != nil {
			t.Fatalf("%s: unexpected validation error %v", name, err)
		}
	}
}

func TestOfficialXianfengVideoLifecycle(t *testing.T) {
	adapter := xianfengAdapter(t)
	request := GenerationRequest{Model: "seedance-2.5", Prompt: "p"}
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task_xf123","object":"video","model":"seedance-2.5","status":"queued","progress":0,"created_at":1793285240}`))
	if err != nil {
		t.Fatal(err)
	}
	if created.TaskID != "task_xf123" || created.Status != StatusPending || created.Result != nil {
		t.Fatalf("xianfeng create result = %#v", created)
	}
	pollContext := PollContext{Request: request, TaskID: created.TaskID}
	poll, err := adapter.BuildPoll(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if poll.Method != "GET" || poll.Path != "/v1/videos/task_xf123" {
		t.Fatalf("xianfeng poll = %#v", poll)
	}
	for payload, stage := range map[string]string{
		`{"id":"task_xf123","status":"queued","progress":0}`:       "上游排队中",
		`{"id":"task_xf123","status":"in_progress","progress":40}`: "上游生成中",
	} {
		pending, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if pending.Status == StatusSucceeded || pending.Status == StatusFailed || pending.Stage != stage {
			t.Fatalf("xianfeng pending %s = %#v", payload, pending)
		}
	}
	const directURL = "https://cdn.example.com/video.mp4"
	done, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_xf123","status":"completed","progress":100,"seconds":"10","url":"`+directURL+`","content_url":"/v1/videos/task_xf123/content"}`))
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != StatusSucceeded || done.Result == nil || len(done.Result.Videos) != 1 || done.Result.Videos[0].URL != directURL {
		t.Fatalf("xianfeng completed with url = %#v", done)
	}
	for _, payload := range []string{
		`{"id":"task_xf123","status":"completed","content_url":"/v1/videos/task_xf123/content"}`,
		`{"id":"task_xf123","status":"completed","url":"/v1/videos/task_xf123/content"}`,
	} {
		relative, err := adapter.ParsePoll(context.Background(), pollContext, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if relative.Status != StatusSucceeded || relative.Result != nil {
			t.Fatalf("xianfeng completed without direct url %s = %#v", payload, relative)
		}
	}
	failed, err := adapter.ParsePoll(context.Background(), pollContext, []byte(`{"id":"task_xf123","status":"failed","error":{"message":"参考素材无法打开","type":"invalid_request_error"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if failed.Status != StatusFailed || failed.Message != "参考素材无法打开" {
		t.Fatalf("xianfeng failed = %#v", failed)
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("xianfeng adapter does not expose result download")
	}
	result, err := resultAdapter.BuildResult(context.Background(), pollContext)
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task_xf123/content" || result.Headers["Accept"] != "video/mp4" || result.Auth.Type != "bearer" {
		t.Fatalf("xianfeng result = %#v", result)
	}
}
