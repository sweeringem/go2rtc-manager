package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-manager/actor"
	"github.com/example/go2rtc-manager/config"
	"github.com/example/go2rtc-manager/httpserver"
	"github.com/example/go2rtc-manager/logging"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := logging.New(cfg.Log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = logger.Sync()
	}()
	system := protoactor.NewActorSystem()
	props := protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor.NewMasterActor(cfg, logger, system.Root)
	})
	masterPID := system.Root.Spawn(props)

	logger.Info("master actor started", zap.String("pid", masterPID.String()))

	httpServer := httpserver.New(cfg, logger, system.Root, masterPID)
	go func() {
		if err := httpServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server stopped with error", zap.Error(err))
		}
	}()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals

	logger.Info("shutdown signal received")
	if err := httpServer.Shutdown(); err != nil {
		logger.Error("http server shutdown failed", zap.Error(err))
	}
	system.Root.Stop(masterPID)
}
