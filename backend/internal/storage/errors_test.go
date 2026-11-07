package storage

import (
	"errors"
	"fmt"
	"net/url"
	"testing"
)

// 地址解析失败发生在发请求之前，不能被当成可重试的网络错误；真正的请求失败仍按网络错误重试。
func TestClassifyErrorURLParseIsNotTransient(t *testing.T) {
	_, parseErr := url.Parse("https://cdn.example/a\nb")
	if parseErr == nil {
		t.Fatal("fixture url must fail to parse")
	}
	if ClassifyError(parseErr).Transient {
		t.Fatalf("parse error classified as transient: %v", parseErr)
	}
	if ClassifyError(fmt.Errorf("wrapped: %w", parseErr)).Transient {
		t.Fatal("wrapped parse error classified as transient")
	}
	requestErr := &url.Error{Op: "Get", URL: "https://cdn.example/a", Err: errors.New("connection refused")}
	if !ClassifyError(requestErr).Transient {
		t.Fatal("request failure must stay transient")
	}
}
