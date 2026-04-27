package actor

import (
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	"go.uber.org/zap"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type StreamCleanerActor struct {
	config config.Config
	logger *zap.Logger

	rootContext *protoactor.RootContext
	go2rtcPID   *protoactor.PID
	masterPID   *protoactor.PID

	pending      map[string]common.StreamHealthChecked
	running      bool
	outstanding  int
	triggeredAt  time.Time
	reason       string
	aliveCount   int
	removedCount int
}

func NewStreamCleanerActor(
	cfg config.Config,
	logger *zap.Logger,
	rootContext *protoactor.RootContext,
	go2rtcPID *protoactor.PID,
	masterPID *protoactor.PID,
) *StreamCleanerActor {
	return &StreamCleanerActor{
		config:      cfg,
		logger:      logger.With(zap.String("actor", "StreamCleanerActor")),
		rootContext: rootContext,
		go2rtcPID:   go2rtcPID,
		masterPID:   masterPID,
		pending:     make(map[string]common.StreamHealthChecked),
	}
}

func (a *StreamCleanerActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *common.CleanupCycleStarted:
		if a.running {
			a.logger.Warn("cleanup cycle ignored because previous cycle is still running", zap.String("reason", msg.Reason))
			return
		}
		a.running = true
		a.outstanding = 0
		a.pending = make(map[string]common.StreamHealthChecked)
		a.triggeredAt = msg.TriggeredAt
		a.reason = msg.Reason
		a.aliveCount = 0
		a.removedCount = 0
		a.logger.Info("cleanup cycle started", zap.String("reason", msg.Reason), zap.Time("triggered_at", msg.TriggeredAt))
		ctx.Send(a.go2rtcPID, &common.FetchStreamList{TriggeredAt: msg.TriggeredAt, RequestedBy: ctx.Self().String()})
	case *common.StreamListFetched:
		if !a.running || msg.RequestedBy != ctx.Self().String() || !msg.TriggeredAt.Equal(a.triggeredAt) {
			return
		}
		if msg.Error != "" {
			a.running = false
			a.outstanding = 0
			a.logger.Error("cleanup cycle aborted because stream list fetch failed", zap.String("error", msg.Error))
			return
		}
		if len(msg.Streams) == 0 {
			a.running = false
			a.outstanding = 0
			a.logger.Info("cleanup cycle finished: no streams found")
			ctx.Send(ctx.Parent(), &common.CleanupCycleFinished{
				TriggeredAt:    a.triggeredAt,
				Reason:         a.reason,
				AliveStreams:   0,
				RemovedStreams: 0,
			})
			return
		}

		a.outstanding = len(msg.Streams)
		for _, streamName := range msg.Streams {
			ctx.Send(a.go2rtcPID, &common.CheckStreamHealth{
				StreamName:   streamName,
				TriggeredAt:  msg.TriggeredAt,
				Attempt:      1,
				RequestedBy:  ctx.Self().String(),
				ConfirmAfter: a.config.Schedule.ConfirmationDelay,
			})
		}
	case *common.StreamHealthChecked:
		if !a.running || msg.RequestedBy != ctx.Self().String() || !msg.TriggeredAt.Equal(a.triggeredAt) {
			return
		}
		a.handleStreamHealth(ctx, msg)
	case *common.StreamRemovalCompleted:
		if !a.running || !msg.TriggeredAt.Equal(a.triggeredAt) {
			return
		}
		a.handleStreamRemovalCompleted(ctx, msg)
	default:
	}
}

func (a *StreamCleanerActor) handleStreamHealth(ctx protoactor.Context, msg *common.StreamHealthChecked) {
	if msg.Error != "" {
		delete(a.pending, msg.StreamName)
		a.logger.Error("stream health check failed",
			zap.String("stream", msg.StreamName),
			zap.Int("attempt", msg.Attempt),
			zap.String("error", msg.Error),
		)
		a.markStreamCompleted(ctx)
		return
	}

	if msg.HasProducer {
		delete(a.pending, msg.StreamName)
		a.aliveCount++
		a.logger.Info("stream healthy", zap.String("stream", msg.StreamName), zap.Int("attempt", msg.Attempt))
		a.markStreamCompleted(ctx)
		return
	}

	if msg.Attempt == 1 {
		a.pending[msg.StreamName] = *msg
		a.logger.Warn("stream missing producer on first attempt",
			zap.String("stream", msg.StreamName),
			zap.Duration("confirm_after", msg.ConfirmAfter),
		)

		go func(streamName string, triggeredAt time.Time, confirmAfter time.Duration, requestedBy string) {
			time.Sleep(confirmAfter)
			a.rootContext.Send(a.go2rtcPID, &common.CheckStreamHealth{
				StreamName:   streamName,
				TriggeredAt:  triggeredAt,
				Attempt:      2,
				RequestedBy:  requestedBy,
				ConfirmAfter: confirmAfter,
			})
		}(msg.StreamName, msg.TriggeredAt, msg.ConfirmAfter, ctx.Self().String())
		return
	}

	if _, exists := a.pending[msg.StreamName]; exists {
		delete(a.pending, msg.StreamName)
		ctx.Send(a.go2rtcPID, &common.RemoveStream{
			StreamName:  msg.StreamName,
			TriggeredAt: msg.TriggeredAt,
		})
		return
	}

	a.markStreamCompleted(ctx)
}

func (a *StreamCleanerActor) handleStreamRemovalCompleted(ctx protoactor.Context, msg *common.StreamRemovalCompleted) {
	if msg.Error != "" {
		a.logger.Error("stream removal failed", zap.String("stream", msg.StreamName), zap.String("error", msg.Error))
	} else if msg.Removed {
		a.removedCount++
		a.logger.Info("stream removal completed", zap.String("stream", msg.StreamName), zap.Time("removed_at", msg.RemovedAt))
	} else {
		a.logger.Info("stream removal skipped because stream already absent", zap.String("stream", msg.StreamName))
	}

	a.markStreamCompleted(ctx)
}

func (a *StreamCleanerActor) markStreamCompleted(ctx protoactor.Context) {
	if a.outstanding > 0 {
		a.outstanding--
	}
	a.finishCycleIfDone(ctx)
}

func (a *StreamCleanerActor) finishCycleIfDone(ctx protoactor.Context) {
	if a.outstanding != 0 {
		return
	}
	if !a.running {
		return
	}

	a.running = false
	a.logger.Info("cleanup cycle finished", zap.Int("alive_streams", a.aliveCount), zap.String("reason", a.reason))
	ctx.Send(ctx.Parent(), &common.CleanupCycleFinished{
		TriggeredAt:    a.triggeredAt,
		Reason:         a.reason,
		AliveStreams:   a.aliveCount,
		RemovedStreams: a.removedCount,
	})
}
