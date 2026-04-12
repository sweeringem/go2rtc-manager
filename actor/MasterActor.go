package actor

import (
	"log/slog"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	cron "github.com/robfig/cron/v3"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type MasterActor struct {
	config      config.Config
	logger      *slog.Logger
	rootContext *protoactor.RootContext

	streamCleanerPID *protoactor.PID
	streamCountPID   *protoactor.PID
	go2rtcPID        *protoactor.PID
	snapshotPID      *protoactor.PID
	actionPID        *protoactor.PID
	redisPID         *protoactor.PID

	cleanupScheduler *cron.Cron
	redisTicker      *time.Ticker
	done             chan struct{}
}

func NewMasterActor(cfg config.Config, logger *slog.Logger, root *protoactor.RootContext) *MasterActor {
	return &MasterActor{
		config:      cfg,
		logger:      logger.With("actor", "MasterActor"),
		rootContext: root,
		done:        make(chan struct{}),
	}
}

func (a *MasterActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.spawnChildren(ctx)
		a.startCleanupScheduler(ctx.Self())
		a.startRedisPublishScheduler(ctx.Self())
	case *protoactor.Stopping:
		if a.cleanupScheduler != nil {
			stopCtx := a.cleanupScheduler.Stop()
			<-stopCtx.Done()
		}
		if a.redisTicker != nil {
			a.redisTicker.Stop()
		}
		close(a.done)
		a.logger.Info("master actor stopping")
	case *common.TriggerCleanup:
		a.logger.Info("routing cleanup trigger", "reason", msg.Reason)
		ctx.Send(a.streamCleanerPID, &common.CleanupCycleStarted{
			TriggeredAt: time.Now(),
			Reason:      msg.Reason,
		})
	case *common.CountAliveStreamsStarted:
		ctx.Send(a.streamCountPID, msg)
	case *common.StreamListFetched:
		ctx.Send(a.streamCleanerPID, msg)
		ctx.Send(a.streamCountPID, msg)
	case *common.StreamHealthChecked:
		ctx.Send(a.streamCleanerPID, msg)
		ctx.Send(a.streamCountPID, msg)
	case *common.StreamRemovalCompleted:
		ctx.Send(a.streamCleanerPID, msg)
	case *common.StreamRemoved:
		a.logger.Warn("stream removed through go2rtc api", "stream", msg.StreamName, "removed_at", msg.RemovedAt)
		ctx.Send(a.actionPID, &common.ExecuteAction{
			StreamName: msg.StreamName,
			Action:     "stream_removed_after_double_check",
		})
	case *common.CleanupCycleFinished:
		a.logger.Info("cleanup cycle summary",
			"reason", msg.Reason,
			"triggered_at", msg.TriggeredAt,
			"alive_streams", msg.AliveStreams,
			"removed_streams", msg.RemovedStreams,
		)
		if msg.RemovedStreams == 0 {
			return
		}
		ctx.Send(a.redisPID, &common.UpdateStreamCount{
			TriggeredAt:  msg.TriggeredAt,
			Reason:       "cleanup_removed_streams",
			AliveStreams: msg.AliveStreams,
		})
	case *common.AliveStreamCountCalculated:
		if msg.Error != "" {
			a.logger.Error("periodic alive stream count failed", "reason", msg.Reason, "error", msg.Error)
			return
		}
		ctx.Send(a.redisPID, &common.UpdateStreamCount{
			TriggeredAt:  msg.TriggeredAt,
			Reason:       msg.Reason,
			AliveStreams: msg.AliveStreams,
		})
	case *common.ExecuteAction:
		ctx.Send(a.actionPID, msg)
	case *common.CaptureSnapshotRequest:
		ctx.Forward(a.snapshotPID)
	default:
	}
}

func (a *MasterActor) spawnChildren(ctx protoactor.Context) {
	a.go2rtcPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewGo2RTCActor(a.config, a.logger)
	}))
	a.actionPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewActionActor(a.config, a.logger)
	}))
	a.snapshotPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewSnapshotActor(a.config, a.logger)
	}))
	a.redisPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewRedisActor(a.config, a.logger)
	}))
	a.streamCleanerPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewStreamCleanerActor(a.config, a.logger, a.rootContext, a.go2rtcPID, ctx.Self())
	}))
	a.streamCountPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewStreamCountActor(a.config, a.logger, a.rootContext, a.go2rtcPID)
	}))

	a.logger.Info("child actors spawned",
		"streamCleanerPID", a.streamCleanerPID.String(),
		"streamCountPID", a.streamCountPID.String(),
		"go2rtcPID", a.go2rtcPID.String(),
		"snapshotPID", a.snapshotPID.String(),
		"actionPID", a.actionPID.String(),
		"redisPID", a.redisPID.String(),
	)
}

func (a *MasterActor) startCleanupScheduler(self *protoactor.PID) {
	a.cleanupScheduler = cron.New(
		cron.WithLocation(time.Local),
		cron.WithParser(cron.NewParser(cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)),
	)

	for _, spec := range a.config.Schedule.Crons {
		spec := spec
		if _, err := a.cleanupScheduler.AddFunc(spec, func() {
			a.rootContext.Send(self, &common.TriggerCleanup{Reason: "scheduled"})
		}); err != nil {
			a.logger.Error("failed to register cleanup cron", "cron", spec, "error", err)
			continue
		}
	}

	a.cleanupScheduler.Start()

	if a.config.Schedule.RunOnStart {
		a.rootContext.Send(self, &common.TriggerCleanup{Reason: "run_on_start"})
	}

	a.logger.Info("cleanup scheduler started",
		"crons", a.config.Schedule.Crons,
		"timezone", time.Local.String(),
		"run_on_start", a.config.Schedule.RunOnStart,
	)
}

func (a *MasterActor) startRedisPublishScheduler(self *protoactor.PID) {
	if a.config.Redis.Addr == "" || a.config.Redis.PublishInterval <= 0 {
		return
	}

	a.redisTicker = time.NewTicker(a.config.Redis.PublishInterval)

	go func() {
		a.rootContext.Send(self, &common.CountAliveStreamsStarted{
			TriggeredAt: time.Now(),
			Reason:      "redis_publish_run_on_start",
		})

		for {
			select {
			case <-a.done:
				return
			case <-a.redisTicker.C:
				a.rootContext.Send(self, &common.CountAliveStreamsStarted{
					TriggeredAt: time.Now(),
					Reason:      "redis_publish_scheduled",
				})
			}
		}
	}()

	a.logger.Info("redis publish scheduler started",
		"interval", a.config.Redis.PublishInterval,
	)
}
