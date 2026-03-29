package actor

import (
	"log/slog"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-stream-cleaner/common"
	"github.com/example/go2rtc-stream-cleaner/config"
)

type MasterActor struct {
	config      config.Config
	logger      *slog.Logger
	rootContext *protoactor.RootContext

	streamCleanerPID *protoactor.PID
	go2rtcPID        *protoactor.PID
	actionPID        *protoactor.PID
	redisPID         *protoactor.PID

	ticker *time.Ticker
	done   chan struct{}
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
		a.startScheduler(ctx.Self())
	case *protoactor.Stopping:
		if a.ticker != nil {
			a.ticker.Stop()
		}
		close(a.done)
		a.logger.Info("master actor stopping")
	case *common.TriggerCleanup:
		a.logger.Info("routing cleanup trigger", "reason", msg.Reason)
		ctx.Send(a.streamCleanerPID, &common.CleanupCycleStarted{
			TriggeredAt: time.Now(),
			Reason:      msg.Reason,
		})
	case *common.StreamListFetched:
		ctx.Send(a.streamCleanerPID, msg)
	case *common.StreamHealthChecked:
		ctx.Send(a.streamCleanerPID, msg)
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
			Reason:       msg.Reason,
			AliveStreams: msg.AliveStreams,
		})
	case *common.ExecuteAction:
		ctx.Send(a.actionPID, msg)
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
	a.redisPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewRedisActor(a.config, a.logger)
	}))
	a.streamCleanerPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return NewStreamCleanerActor(a.config, a.logger, a.rootContext, a.go2rtcPID, ctx.Self())
	}))

	a.logger.Info("child actors spawned",
		"streamCleanerPID", a.streamCleanerPID.String(),
		"go2rtcPID", a.go2rtcPID.String(),
		"actionPID", a.actionPID.String(),
		"redisPID", a.redisPID.String(),
	)
}

func (a *MasterActor) startScheduler(self *protoactor.PID) {
	a.ticker = time.NewTicker(a.config.Schedule.Interval)

	go func() {
		if a.config.Schedule.RunOnStart {
			a.rootContext.Send(self, &common.TriggerCleanup{Reason: "run_on_start"})
		}

		for {
			select {
			case <-a.done:
				return
			case <-a.ticker.C:
				a.rootContext.Send(self, &common.TriggerCleanup{Reason: "scheduled"})
			}
		}
	}()

	a.logger.Info("scheduler started",
		"interval", a.config.Schedule.Interval,
		"run_on_start", a.config.Schedule.RunOnStart,
	)
}
