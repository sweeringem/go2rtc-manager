package actor

import (
	"io"
	"log/slog"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/example/go2rtc-stream-cleaner/config"
)

func TestRedisActorSetAliveStreamCount(t *testing.T) {
	t.Parallel()

	redisServer, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer redisServer.Close()

	actor := NewRedisActor(config.Config{
		Redis: config.RedisConfig{
			Addr:     redisServer.Addr(),
			DB:       0,
			RouterIP: "192.168.0.1",
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	if err := actor.setAliveStreamCount(7); err != nil {
		t.Fatalf("setAliveStreamCount: %v", err)
	}

	value, err := redisServer.Get("steam@count@192.168.0.1")
	if err != nil {
		t.Fatalf("get redis value: %v", err)
	}
	if value != "7" {
		t.Fatalf("unexpected redis value: got %s want %s", value, "7")
	}
}
