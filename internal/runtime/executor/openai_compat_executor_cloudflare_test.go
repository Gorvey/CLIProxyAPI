package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
)

func TestCloudflareRetryAfterUsesNextUTCMidnightPlusFiveMinutes(t *testing.T) {
	auth := &cliproxyauth.Auth{Attributes: map[string]string{
		"base_url": "https://api.cloudflare.example/accounts/123/ai/v1",
	}}
	now := time.Date(2026, 3, 26, 19, 25, 8, 0, time.FixedZone("UTC+8", 8*60*60))

	retryAfter, unlockAt, ok := cloudflareRetryAfter(auth, http.StatusTooManyRequests, now)
	if !ok {
		t.Fatal("expected cloudflare cooldown to be detected")
	}

	wantUnlock := time.Date(2026, 3, 27, 0, 5, 0, 0, time.UTC)
	if !unlockAt.Equal(wantUnlock) {
		t.Fatalf("unlockAt = %v, want %v", unlockAt, wantUnlock)
	}
	wantRetryAfter := wantUnlock.Sub(now)
	if retryAfter != wantRetryAfter {
		t.Fatalf("retryAfter = %v, want %v", retryAfter, wantRetryAfter)
	}
}

func TestOpenAICompatExecutorExecuteCloudflare429SetsRetryAfterAndLogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) == 0 {
			t.Fatal("expected request body")
		}
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota"}`))
	}))
	defer server.Close()

	restoreLogs, logBuf := captureStandardLogger(t)
	defer restoreLogs()

	executor := NewOpenAICompatExecutor("cf-provider-1", &config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "cf-provider-1",
		Attributes: map[string]string{
			"base_url": server.URL + "/cloudflare/v1",
			"api_key":  "test-key",
		},
	}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "kimi-k2.5",
		Payload: []byte(`{"model":"kimi-k2.5","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
	})
	if err == nil {
		t.Fatal("expected Execute to return error")
	}

	se, ok := err.(statusErr)
	if !ok {
		t.Fatalf("error type = %T, want statusErr", err)
	}
	if se.retryAfter == nil || *se.retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive duration", se.retryAfter)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "cf provider disabled") {
		t.Fatalf("expected cooldown log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "provider=cf-provider-1") {
		t.Fatalf("expected provider in log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "model=kimi-k2.5") {
		t.Fatalf("expected model in log, got %q", logOutput)
	}
	if !strings.Contains(logOutput, "unlock") {
		t.Fatalf("expected unlock time in log, got %q", logOutput)
	}
}

func TestOpenAICompatExecutorExecuteStreamCloudflare429SetsRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota"}`))
	}))
	defer server.Close()

	executor := NewOpenAICompatExecutor("cf-provider-1", &config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "cf-provider-1",
		Attributes: map[string]string{
			"base_url": server.URL + "/cloudflare/v1",
			"api_key":  "test-key",
		},
	}
	_, err := executor.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "kimi-k2.5",
		Payload: []byte(`{"model":"kimi-k2.5","messages":[{"role":"user","content":"hi"}],"stream":true}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
		Stream:       true,
	})
	if err == nil {
		t.Fatal("expected ExecuteStream to return error")
	}

	se, ok := err.(statusErr)
	if !ok {
		t.Fatalf("error type = %T, want statusErr", err)
	}
	if se.retryAfter == nil || *se.retryAfter <= 0 {
		t.Fatalf("retryAfter = %v, want positive duration", se.retryAfter)
	}
}

func TestOpenAICompatExecutorExecuteNonCloudflare429DoesNotSetRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"quota"}`))
	}))
	defer server.Close()

	restoreLogs, logBuf := captureStandardLogger(t)
	defer restoreLogs()

	executor := NewOpenAICompatExecutor("generic-provider", &config.Config{})
	auth := &cliproxyauth.Auth{
		Provider: "generic-provider",
		Attributes: map[string]string{
			"base_url": server.URL + "/v1",
			"api_key":  "test-key",
		},
	}
	_, err := executor.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "kimi-k2.5",
		Payload: []byte(`{"model":"kimi-k2.5","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("openai"),
	})
	if err == nil {
		t.Fatal("expected Execute to return error")
	}

	se, ok := err.(statusErr)
	if !ok {
		t.Fatalf("error type = %T, want statusErr", err)
	}
	if se.retryAfter != nil {
		t.Fatalf("retryAfter = %v, want nil", *se.retryAfter)
	}
	if strings.Contains(logBuf.String(), "cf provider disabled") {
		t.Fatalf("unexpected cloudflare cooldown log: %q", logBuf.String())
	}
}

func captureStandardLogger(t *testing.T) (func(), *bytes.Buffer) {
	t.Helper()

	logger := log.StandardLogger()
	prevOut := logger.Out
	prevFormatter := logger.Formatter
	prevLevel := logger.Level
	buf := &bytes.Buffer{}

	logger.SetOutput(buf)
	logger.SetFormatter(&log.TextFormatter{
		DisableTimestamp: true,
		DisableColors:    true,
	})
	logger.SetLevel(log.WarnLevel)

	return func() {
		logger.SetOutput(prevOut)
		logger.SetFormatter(prevFormatter)
		logger.SetLevel(prevLevel)
	}, buf
}
