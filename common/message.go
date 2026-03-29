package common

import "time"

type StartScheduler struct{}

type TriggerCleanup struct {
	Reason string
}

type CleanupCycleStarted struct {
	TriggeredAt time.Time
	Reason      string
}

type FetchStreamList struct {
	TriggeredAt time.Time
}

type StreamListFetched struct {
	TriggeredAt time.Time
	Streams     []string
	Error       string
}

type CheckStreamHealth struct {
	StreamName   string
	TriggeredAt  time.Time
	Attempt      int
	RequestedBy  string
	ConfirmAfter time.Duration
}

type StreamHealthChecked struct {
	StreamName   string
	TriggeredAt  time.Time
	Attempt      int
	HasProducer  bool
	CheckedAt    time.Time
	ConfirmAfter time.Duration
	Error        string
}

type RemoveStream struct {
	StreamName  string
	TriggeredAt time.Time
}

type StreamRemoved struct {
	StreamName string
	RemovedAt  time.Time
}

type StreamRemovalCompleted struct {
	StreamName  string
	TriggeredAt time.Time
	Removed     bool
	RemovedAt   time.Time
	Error       string
}

type ExecuteAction struct {
	StreamName string
	Action     string
}

type CleanupCycleFinished struct {
	TriggeredAt    time.Time
	Reason         string
	AliveStreams   int
	RemovedStreams int
}

type UpdateStreamCount struct {
	TriggeredAt  time.Time
	Reason       string
	AliveStreams int
}
