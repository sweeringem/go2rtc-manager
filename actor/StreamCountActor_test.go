package actor

import (
	"io"
	"log/slog"
	"testing"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type go2rtcCountStubActor struct {
	root     *protoactor.RootContext
	countPID **protoactor.PID
}

func (a *go2rtcCountStubActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *common.FetchStreamList:
		a.root.Send(*a.countPID, &common.StreamListFetched{
			TriggeredAt: msg.TriggeredAt,
			RequestedBy: msg.RequestedBy,
			Streams:     []string{"cam-a", "cam-b"},
		})
	case *common.CheckStreamHealth:
		a.root.Send(*a.countPID, &common.StreamHealthChecked{
			StreamName:   msg.StreamName,
			TriggeredAt:  msg.TriggeredAt,
			Attempt:      msg.Attempt,
			RequestedBy:  msg.RequestedBy,
			ConfirmAfter: msg.ConfirmAfter,
			HasProducer:  msg.StreamName == "cam-a",
		})
	}
}

type streamCountHarnessActor struct {
	root    *protoactor.RootContext
	logger  *slog.Logger
	cfg     config.Config
	results chan *common.AliveStreamCountCalculated

	go2rtcPID *protoactor.PID
	countPID  *protoactor.PID
}

func (a *streamCountHarnessActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.go2rtcPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
			return &go2rtcCountStubActor{root: a.root, countPID: &a.countPID}
		}))
		a.countPID = ctx.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
			return NewStreamCountActor(a.cfg, a.logger, a.root, a.go2rtcPID)
		}))
	case *common.CountAliveStreamsStarted:
		ctx.Send(a.countPID, msg)
	case *common.AliveStreamCountCalculated:
		a.results <- msg
	}
}

func TestStreamCountActorCountsAliveStreams(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	results := make(chan *common.AliveStreamCountCalculated, 1)
	harnessPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &streamCountHarnessActor{
			root:    system.Root,
			logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
			cfg:     config.Config{Schedule: config.ScheduleConfig{ConfirmationDelay: time.Millisecond}},
			results: results,
		}
	}))

	triggeredAt := time.Now()
	system.Root.Send(harnessPID, &common.CountAliveStreamsStarted{TriggeredAt: triggeredAt, Reason: "test"})

	select {
	case msg := <-results:
		if msg.Error != "" {
			t.Fatalf("unexpected error: %s", msg.Error)
		}
		if msg.AliveStreams != 1 {
			t.Fatalf("unexpected alive count: got %d want %d", msg.AliveStreams, 1)
		}
		if msg.Reason != "test" {
			t.Fatalf("unexpected reason: got %s want %s", msg.Reason, "test")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for count result")
	}
}
