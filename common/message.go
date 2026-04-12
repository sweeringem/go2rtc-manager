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

type CountAliveStreamsStarted struct {
	TriggeredAt time.Time
	Reason      string
}

type FetchStreamList struct {
	TriggeredAt time.Time
	RequestedBy string
}

type StreamListFetched struct {
	TriggeredAt time.Time
	Streams     []string
	RequestedBy string
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
	RequestedBy  string
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

type AliveStreamCountCalculated struct {
	TriggeredAt  time.Time
	Reason       string
	AliveStreams int
	Error        string
}

type UpdateStreamCount struct {
	TriggeredAt  time.Time
	Reason       string
	AliveStreams int
}

type CaptureSnapshotRequest struct {
	CamID       string
	RequestedAt time.Time
}

type CaptureSnapshotResult struct {
	CamID       string
	RequestedAt time.Time
	SavedPath   string
	StatusCode  int
	Error       string
}
