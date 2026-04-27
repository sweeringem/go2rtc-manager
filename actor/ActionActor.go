package actor

import (
	protoactor "github.com/asynkron/protoactor-go/actor"
	"go.uber.org/zap"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type ActionActor struct {
	config config.Config
	logger *zap.Logger
}

func NewActionActor(cfg config.Config, logger *zap.Logger) *ActionActor {
	return &ActionActor{
		config: cfg,
		logger: logger.With(zap.String("actor", "ActionActor")),
	}
}

func (a *ActionActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.logger.Info("action actor started", zap.Bool("dry_run", a.config.Action.DryRun))
	case *common.ExecuteAction:
		a.logger.Warn("follow-up action requested",
			zap.String("stream", msg.StreamName),
			zap.String("action", msg.Action),
			zap.Bool("dry_run", a.config.Action.DryRun),
		)
	default:
	}
}
