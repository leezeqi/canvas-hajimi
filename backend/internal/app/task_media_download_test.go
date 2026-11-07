package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withMediaDownloadTimeouts(t *testing.T, maxDuration, stall time.Duration) {
	t.Helper()
	previousMax, previousStall := mediaDownloadMaxDuration, mediaDownloadStallTimeout
	mediaDownloadMaxDuration, mediaDownloadStallTimeout = maxDuration, stall
	t.Cleanup(func() { mediaDownloadMaxDuration, mediaDownloadStallTimeout = previousMax, previousStall })
}

// 慢速结果服务器：总耗时远超停滞阈值，但每段之间都有进度，必须完整下载而不是被总时限截断。
func TestDownloadTaskMediaFinishesSlowButSteadyUpstream(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	withMediaDownloadTimeouts(t, 10*time.Second, 300*time.Millisecond)
	s, _ := newMediaRecoveryTestService(t)
	picture := mediaTestPNG(t)
	padded := append(append([]byte{}, picture...), bytes.Repeat([]byte{0}, 4096)...)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for start := 0; start < len(padded); start += 512 {
			end := min(start+512, len(padded))
			_, _ = w.Write(padded[start:end])
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer server.Close()

	started := time.Now()
	name, mimeType, err := s.downloadTaskMedia(context.Background(), providerConfig{}, server.URL+"/result.png", "image")
	if err != nil {
		t.Fatalf("slow but steady download failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 3*mediaDownloadStallTimeout {
		t.Fatalf("fixture finished too fast to prove the stall guard resets: %s", elapsed)
	}
	if mimeType != "image/png" {
		t.Fatalf("mime=%s", mimeType)
	}
	data, err := os.ReadFile(s.mediaTempPath(name))
	if err != nil || !bytes.Equal(data, padded) {
		t.Fatalf("staged file mismatch: %v len=%d want=%d", err, len(data), len(padded))
	}
}

// 上游发出响应头和部分内容后不再传输：按停滞放弃，错误可重试，且不必等满总时限。
func TestDownloadTaskMediaStopsStalledUpstreamAsRetryable(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	withMediaDownloadTimeouts(t, 10*time.Second, 200*time.Millisecond)
	s, _ := newMediaRecoveryTestService(t)
	picture := mediaTestPNG(t)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(picture[:8])
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)

	started := time.Now()
	_, _, err := s.downloadTaskMedia(context.Background(), providerConfig{}, server.URL+"/result.png", "image")
	if !errors.Is(err, errMediaDownloadStalled) {
		t.Fatalf("err=%v, want stalled", err)
	}
	if !retryableMediaRecovery(err) {
		t.Fatal("a stalled download must be retried, not treated as a permanent failure")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("stalled download waited for the total deadline: %s", elapsed)
	}
	entries, err := os.ReadDir(filepath.Join(s.dataDir, "media-staging"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("partial staging file left behind: %d %v", len(entries), err)
	}
}

// 任务租约失效、服务停止等外部取消不能被说成上游卡住。
func TestDownloadTaskMediaKeepsExternalCancellation(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	withMediaDownloadTimeouts(t, 10*time.Second, 5*time.Second)
	s, _ := newMediaRecoveryTestService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, _, err := s.downloadTaskMedia(ctx, providerConfig{}, server.URL+"/result.png", "image")
	if err == nil || errors.Is(err, errMediaDownloadStalled) {
		t.Fatalf("err=%v, want the caller's cancellation", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}

func TestMediaURLCandidates(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want []string
	}{
		{" https://a.example/x.mp4 ", []string{"https://a.example/x.mp4"}},
		{"https://a.example/v/1/content\nhttps://cdn.example/x.mp4?a=1&b=2", []string{"https://a.example/v/1/content", "https://cdn.example/x.mp4?a=1&b=2"}},
		{"https://a.example/x.mp4\n not-a-url", []string{"https://a.example/x.mp4\n not-a-url"}},
		{"", []string{""}},
	} {
		got := mediaURLCandidates(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("%q => %q, want %q", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%q => %q, want %q", tc.raw, got, tc.want)
			}
		}
	}
}

// 上游在 url 字段里用换行拼接多个候选地址（Navivo）：第一个不可用时必须改用下一个，而不是把整串当成一个地址。
func TestDownloadTaskMediaTriesNewlineSeparatedCandidates(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	s, _ := newMediaRecoveryTestService(t)
	picture := mediaTestPNG(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/result.png" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(picture)
	}))
	defer server.Close()

	name, mimeType, err := s.downloadTaskMedia(context.Background(), providerConfig{}, server.URL+"/v1/videos/x/content\n"+server.URL+"/result.png", "image")
	if err != nil {
		t.Fatalf("candidate download failed: %v", err)
	}
	if mimeType != "image/png" {
		t.Fatalf("mime=%s", mimeType)
	}
	if data, err := os.ReadFile(s.mediaTempPath(name)); err != nil || !bytes.Equal(data, picture) {
		t.Fatalf("staged file mismatch: %v", err)
	}
}

// 无法解析的地址在发请求前就失败，重试必然失败：不能进入自动恢复空等，并且日志原因要可辨认。
func TestDownloadTaskMediaInvalidURLIsPermanent(t *testing.T) {
	s, _ := newMediaRecoveryTestService(t)
	_, _, err := s.downloadTaskMedia(context.Background(), providerConfig{}, "https://cdn.example/a\x01b.mp4", "video")
	if err == nil {
		t.Fatal("invalid url must fail")
	}
	if retryableMediaRecovery(err) {
		t.Fatalf("url parse error must not be retried: %v", err)
	}
	if got := mediaDownloadFailureDetail(err); got != "上游结果地址无效" {
		t.Fatalf("detail=%q", got)
	}
}

// 所有候选都失败时，只要有一个是临时错误就保留自动重试。
func TestDownloadTaskMediaKeepsRetryableCandidateFailure(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	s, _ := newMediaRecoveryTestService(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/busy" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, _, err := s.downloadTaskMedia(context.Background(), providerConfig{}, server.URL+"/gone\n"+server.URL+"/busy", "video")
	if err == nil || !retryableMediaRecovery(err) {
		t.Fatalf("err=%v, want retryable", err)
	}
	if got := mediaDownloadFailureDetail(err); got != "上游返回 HTTP 503" {
		t.Fatalf("detail=%q", got)
	}
}

func TestMediaDownloadLogErrorIsRedacted(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{errMediaDownloadStalled, "结果下载长时间没有进度"},
		{context.DeadlineExceeded, "结果下载超时"},
		{errors.New(`Get "http://example.test/result.png?token=secret": connection reset`), "结果下载失败"},
	} {
		if got := mediaDownloadLogError(tc.err).Error(); got != tc.want {
			t.Fatalf("%v => %s, want %s", tc.err, got, tc.want)
		}
	}
}
