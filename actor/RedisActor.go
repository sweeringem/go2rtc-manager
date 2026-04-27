package actor

import (
	"context"
	"fmt"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type RedisActor struct {
	config config.Config
	logger *zap.Logger
	client *redis.Client
}

func NewRedisActor(cfg config.Config, logger *zap.Logger) *RedisActor {
	return &RedisActor{
		config: cfg,
		logger: logger.With(zap.String("actor", "RedisActor")),
	}
}

func (a *RedisActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		if !a.isEnabled() {
			a.logger.Info("redis actor disabled", zap.String("reason", "redis.addr not configured"))
			return
		}
		if _, err := a.getClient(); err != nil {
			a.logger.Error("failed to initialize redis client", zap.Error(err))
			return
		}
		a.logger.Info("redis actor started",
			zap.String("addr", a.config.Redis.Addr),
			zap.String("box_ip", a.config.App.BoxIP),
			zap.String("key", a.streamCountKey()),
		)
	case *protoactor.Stopping:
		if a.client != nil {
			if err := a.client.Close(); err != nil {
				a.logger.Error("failed to close redis client", zap.Error(err))
			}
		}
	case *common.UpdateStreamCount:
		if !a.isEnabled() {
			return
		}
		if err := a.setAliveStreamCount(msg.AliveStreams); err != nil {
			a.logger.Error("failed to publish alive stream count to redis",
				zap.String("key", a.streamCountKey()),
				zap.Int("alive_streams", msg.AliveStreams),
				zap.String("reason", msg.Reason),
				zap.Error(err),
			)
			return
		}

		a.logger.Info("alive stream count published to redis",
			zap.String("key", a.streamCountKey()),
			zap.Int("alive_streams", msg.AliveStreams),
			zap.String("reason", msg.Reason),
			zap.Time("triggered_at", msg.TriggeredAt),
		)
	default:
	}
}

func (a *RedisActor) isEnabled() bool {
	return a.config.Redis.Addr != ""
}

func (a *RedisActor) getClient() (*redis.Client, error) {
	if !a.isEnabled() {
		return nil, fmt.Errorf("redis is disabled")
	}
	if a.client != nil {
		return a.client, nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:         a.config.Redis.Addr,
		Password:     a.config.Redis.Password,
		DB:           a.config.Redis.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	a.client = client
	return a.client, nil
}

func (a *RedisActor) streamCountKey() string {
	return fmt.Sprintf("stream_count@%s", a.config.App.BoxIP)
}

func (a *RedisActor) setAliveStreamCount(aliveStreams int) error {
	client, err := a.getClient()
	if err != nil {
		return err
	}

	if err := client.Set(context.Background(), a.streamCountKey(), aliveStreams, 0).Err(); err != nil {
		return fmt.Errorf("set redis key: %w", err)
	}

	return nil
}
