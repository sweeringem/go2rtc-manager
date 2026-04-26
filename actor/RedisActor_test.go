package actor

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"

	"github.com/example/go2rtc-manager/config"
)

func TestRedisActorSetAliveStreamCount(t *testing.T) {
	t.Parallel()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer redisServer.Close()

	actor := NewRedisActor(config.Config{
		App: config.AppConfig{
			BoxIP: "192.168.0.10",
		},
		Redis: config.RedisConfig{
			Addr: redisServer.Addr(),
			DB:   0,
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := actor.setAliveStreamCount(7); err != nil {
		t.Fatalf("setAliveStreamCount: %v", err)
	}

	value, err := redisServer.Get("stream_count@192.168.0.10")
	if err != nil {
		t.Fatalf("get redis value: %v", err)
	}
	if value != "7" {
		t.Fatalf("unexpected redis value: got %s want %s", value, "7")
	}
}

func TestNormalizeBucketName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		process string
		want    string
		wantErr bool
	}{
		{name: "lowercases and replaces underscores", process: " BodyCam_PROD ", want: "bodycam-prod"},
		{name: "keeps valid bucket", process: "bodycam.prod", want: "bodycam.prod"},
		{name: "rejects too short", process: "AB", wantErr: true},
		{name: "rejects invalid character", process: "BODYCAM@PROD", wantErr: true},
		{name: "rejects ip address", process: "192.168.0.10", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeBucketName(tt.process)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeBucketName: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected bucket: got %q want %q", got, tt.want)
			}
		})
	}
}

func TestBuildRecordObjectKey(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, 4, 26, 6, 30, 0, 0, time.UTC)
	objectKey, filename, err := buildRecordObjectKey("site/a", "group\\b", "TEST_P1000HDKFH", "UI", startedAt)
	if err != nil {
		t.Fatalf("buildRecordObjectKey: %v", err)
	}

	if filename != "TEST_P1000HDKFH_UI_20260426T063000Z.mp4" {
		t.Fatalf("unexpected filename: got %q", filename)
	}
	if objectKey != "site_a/group_b/TEST_P1000HDKFH_UI_20260426T063000Z.mp4" {
		t.Fatalf("unexpected object key: got %q", objectKey)
	}
}
