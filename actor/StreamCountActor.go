package actor

import (
	"log/slog"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type StreamCountActor struct {
	config config.Config
	logger *slog.Logger

	rootContext *protoactor.RootContext
	go2rtcPID   *protoactor.PID

	pending     map[string]common.StreamHealthChecked
	running     bool
	outstanding int
	triggeredAt time.Time
	reason      string
	aliveCount  int
}

func NewStreamCountActor(
	cfg config.Config,
	logger *slog.Logger,
	rootContext *protoactor.RootContext,
	go2rtcPID *protoactor.PID,
) *StreamCountActor {
	return &StreamCountActor{
		config:      cfg,
		logger:      logger.With("actor", "StreamCountActor"),
		rootContext: rootContext,
		go2rtcPID:   go2rtcPID,
		pending:     make(map[string]common.StreamHealthChecked),
	}
}

func (a *StreamCountActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *common.CountAliveStreamsStarted:
		if a.running {
			a.logger.Warn("alive stream count request ignored because previous request is still running", "reason", msg.Reason)
			return
		}
		a.running = true
		a.outstanding = 0
		a.pending = make(map[string]common.StreamHealthChecked)
		a.triggeredAt = msg.TriggeredAt
		a.reason = msg.Reason
		a.aliveCount = 0
		a.logger.Info("alive stream count started", "reason", msg.Reason, "triggered_at", msg.TriggeredAt)
		ctx.Send(a.go2rtcPID, &common.FetchStreamList{TriggeredAt: msg.TriggeredAt, RequestedBy: ctx.Self().String()})
	case *common.StreamListFetched:
		if !a.running || msg.RequestedBy != ctx.Self().String() || !msg.TriggeredAt.Equal(a.triggeredAt) {
			return
		}
		if msg.Error != "" {
			a.finishWithError(ctx, msg.Error)
			return
		}
		if len(msg.Streams) == 0 {
			a.finish(ctx)
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
	default:
	}
}

func (a *StreamCountActor) handleStreamHealth(ctx protoactor.Context, msg *common.StreamHealthChecked) {
	if msg.Error != "" {
		delete(a.pending, msg.StreamName)
		a.finishWithError(ctx, msg.Error)
		return
	}

	if msg.HasProducer {
		delete(a.pending, msg.StreamName)
		a.aliveCount++
		a.markStreamCompleted(ctx)
		return
	}

	if msg.Attempt == 1 {
		a.pending[msg.StreamName] = *msg
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

	delete(a.pending, msg.StreamName)
	a.markStreamCompleted(ctx)
}

func (a *StreamCountActor) markStreamCompleted(ctx protoactor.Context) {
	if a.outstanding > 0 {
		a.outstanding--
	}
	if a.outstanding == 0 && a.running {
		a.finish(ctx)
	}
}

func (a *StreamCountActor) finish(ctx protoactor.Context) {
	a.running = false
	ctx.Send(ctx.Parent(), &common.AliveStreamCountCalculated{
		TriggeredAt:  a.triggeredAt,
		Reason:       a.reason,
		AliveStreams: a.aliveCount,
	})
}

func (a *StreamCountActor) finishWithError(ctx protoactor.Context, err string) {
	a.running = false
	a.pending = make(map[string]common.StreamHealthChecked)
	a.outstanding = 0
	a.logger.Error("alive stream count failed", "reason", a.reason, "error", err)
	ctx.Send(ctx.Parent(), &common.AliveStreamCountCalculated{
		TriggeredAt: a.triggeredAt,
		Reason:      a.reason,
		Error:       err,
	})
}
