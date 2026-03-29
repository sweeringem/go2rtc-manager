package actor

import (
	"log/slog"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-stream-cleaner/common"
	"github.com/example/go2rtc-stream-cleaner/config"
)

type ActionActor struct {
	config config.Config
	logger *slog.Logger
}

func NewActionActor(cfg config.Config, logger *slog.Logger) *ActionActor {
	return &ActionActor{
		config: cfg,
		logger: logger.With("actor", "ActionActor"),
	}
}

func (a *ActionActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.logger.Info("action actor started", "dry_run", a.config.Action.DryRun)
	case *common.ExecuteAction:
		a.logger.Warn("follow-up action requested",
			"stream", msg.StreamName,
			"action", msg.Action,
			"dry_run", a.config.Action.DryRun,
		)
	default:
	}
}
