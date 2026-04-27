package actor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

func TestRecordActorCompletesRecordJob(t *testing.T) {
	t.Parallel()

	var gotPath string
	var gotSrc string
	var gotDuration string
	var gotFilename string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotSrc = r.URL.Query().Get("src")
		gotDuration = r.URL.Query().Get("duration")
		gotFilename = r.URL.Query().Get("filename")
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("mp4-data"))
	}))
	defer server.Close()

	store := &recordActorFakeStore{}
	system := protoactor.NewActorSystem()
	core, logs := observer.New(zap.InfoLevel)
	actor := newTestRecordActor(system.Root, server.URL, recordActorFakeLookup{
		info: bodycamInfo{
			Process: "BODYCAM_PROD",
			Site:    "site-a",
			Group:   "group-1",
		},
	}, store)
	actor.logger = zap.New(core)
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	result := startTestRecordJob(t, system.Root, pid)
	if result.StatusCode != http.StatusAccepted {
		t.Fatalf("unexpected status code: got %d want %d", result.StatusCode, http.StatusAccepted)
	}

	status := waitForRecordStatus(t, system.Root, pid, result.JobID, recordStatusCompleted)
	if status.Bucket != "bodycam-prod" {
		t.Fatalf("unexpected bucket: got %q", status.Bucket)
	}
	if status.ContentType != recordContentType {
		t.Fatalf("unexpected content type: got %q", status.ContentType)
	}
	if !strings.HasPrefix(status.ObjectKey, "site-a/group-1/TEST_P1000HDKFH_UI_") || !strings.HasSuffix(status.ObjectKey, ".mp4") {
		t.Fatalf("unexpected object key: got %q", status.ObjectKey)
	}

	if gotPath != "/api/stream.mp4" {
		t.Fatalf("unexpected go2rtc path: got %q", gotPath)
	}
	if gotSrc != "TEST_P1000HDKFH" {
		t.Fatalf("unexpected src: got %q", gotSrc)
	}
	if gotDuration != "10" {
		t.Fatalf("unexpected duration: got %q", gotDuration)
	}
	if !strings.HasPrefix(gotFilename, "TEST_P1000HDKFH_UI_") || !strings.HasSuffix(gotFilename, ".mp4") {
		t.Fatalf("unexpected filename: got %q", gotFilename)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ensuredBucket != "bodycam-prod" {
		t.Fatalf("unexpected ensured bucket: got %q", store.ensuredBucket)
	}
	if store.uploadedBucket != "bodycam-prod" {
		t.Fatalf("unexpected uploaded bucket: got %q", store.uploadedBucket)
	}
	if store.uploadedKey != status.ObjectKey {
		t.Fatalf("unexpected uploaded key: got %q want %q", store.uploadedKey, status.ObjectKey)
	}
	if store.uploadedBody != "mp4-data" {
		t.Fatalf("unexpected uploaded body: got %q", store.uploadedBody)
	}

	for _, message := range []string{
		"record job started",
		"record bodycam info loaded",
		"record stream received from go2rtc",
		"record bucket ready",
		"record upload completed",
	} {
		if logs.FilterMessage(message).Len() != 1 {
			t.Fatalf("expected %q log, got %d", message, logs.FilterMessage(message).Len())
		}
	}
}

func TestRecordActorFailsWhenBodycamInfoNotFound(t *testing.T) {
	t.Parallel()

	store := &recordActorFakeStore{}
	system := protoactor.NewActorSystem()
	actor := newTestRecordActor(system.Root, "http://127.0.0.1:1", recordActorFakeLookup{err: errors.New("mongo bodycam info not found")}, store)
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	result := startTestRecordJob(t, system.Root, pid)
	status := waitForRecordStatus(t, system.Root, pid, result.JobID, recordStatusFailed)
	if status.Error != "mongo bodycam info not found" {
		t.Fatalf("unexpected error: got %q", status.Error)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ensureCalls != 0 || store.putCalls != 0 {
		t.Fatalf("store should not be called: ensure=%d put=%d", store.ensureCalls, store.putCalls)
	}
}

func TestRecordActorFailsWhenGo2RTCReturnsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	store := &recordActorFakeStore{}
	system := protoactor.NewActorSystem()
	actor := newTestRecordActor(system.Root, server.URL, recordActorFakeLookup{
		info: bodycamInfo{Process: "bodycam-prod", Site: "site-a", Group: "group-1"},
	}, store)
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	result := startTestRecordJob(t, system.Root, pid)
	status := waitForRecordStatus(t, system.Root, pid, result.JobID, recordStatusFailed)
	if !strings.Contains(status.Error, "unexpected go2rtc record status 500") {
		t.Fatalf("unexpected error: got %q", status.Error)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ensureCalls != 0 || store.putCalls != 0 {
		t.Fatalf("store should not be called: ensure=%d put=%d", store.ensureCalls, store.putCalls)
	}
}

func TestRecordActorFailsWhenEnsureBucketFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mp4-data"))
	}))
	defer server.Close()

	store := &recordActorFakeStore{ensureErr: errors.New("ensure failed")}
	system := protoactor.NewActorSystem()
	actor := newTestRecordActor(system.Root, server.URL, recordActorFakeLookup{
		info: bodycamInfo{Process: "bodycam-prod", Site: "site-a", Group: "group-1"},
	}, store)
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	result := startTestRecordJob(t, system.Root, pid)
	status := waitForRecordStatus(t, system.Root, pid, result.JobID, recordStatusFailed)
	if status.Error != "ensure failed" {
		t.Fatalf("unexpected error: got %q", status.Error)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ensureCalls != 1 || store.putCalls != 0 {
		t.Fatalf("unexpected store calls: ensure=%d put=%d", store.ensureCalls, store.putCalls)
	}
}

func TestRecordActorFailsWhenPutObjectFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mp4-data"))
	}))
	defer server.Close()

	store := &recordActorFakeStore{putErr: errors.New("put failed")}
	system := protoactor.NewActorSystem()
	actor := newTestRecordActor(system.Root, server.URL, recordActorFakeLookup{
		info: bodycamInfo{Process: "bodycam-prod", Site: "site-a", Group: "group-1"},
	}, store)
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	result := startTestRecordJob(t, system.Root, pid)
	status := waitForRecordStatus(t, system.Root, pid, result.JobID, recordStatusFailed)
	if status.Error != "put failed" {
		t.Fatalf("unexpected error: got %q", status.Error)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.ensureCalls != 1 || store.putCalls != 1 {
		t.Fatalf("unexpected store calls: ensure=%d put=%d", store.ensureCalls, store.putCalls)
	}
}

func TestRecordActorRejectsWhenActiveJobsReachLimit(t *testing.T) {
	t.Parallel()

	actor := &RecordActor{
		config: config.Config{Record: config.RecordConfig{MaxConcurrentJobs: 1}},
		logger: zap.NewNop(),
		jobs: map[string]recordJob{
			"running-job": {Status: recordStatusRunning},
		},
	}

	system := protoactor.NewActorSystem()
	pid := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return actor
	}))

	future := system.Root.RequestFuture(pid, &common.StartRecordRequest{
		Type:     "UI",
		Mac:      "AB:CD:EF:DD:GG",
		CamID:    "TEST_P1000HDKFH",
		Duration: 10 * time.Second,
	}, time.Second)
	response, err := future.Result()
	if err != nil {
		t.Fatalf("future result: %v", err)
	}

	result, ok := response.(*common.StartRecordResult)
	if !ok {
		t.Fatalf("unexpected response type: %T", response)
	}
	if result.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("unexpected status code: got %d want %d", result.StatusCode, http.StatusTooManyRequests)
	}
	if result.Error != "too many active record jobs" {
		t.Fatalf("unexpected error: got %q", result.Error)
	}
}

func TestRecordActorActiveRecordJobsIgnoresCompletedAndFailed(t *testing.T) {
	t.Parallel()

	actor := &RecordActor{
		jobs: map[string]recordJob{
			"accepted-job":  {Status: recordStatusAccepted},
			"running-job":   {Status: recordStatusRunning},
			"completed-job": {Status: recordStatusCompleted},
			"failed-job":    {Status: recordStatusFailed},
		},
	}

	if got, want := actor.activeRecordJobs(), 2; got != want {
		t.Fatalf("unexpected active job count: got %d want %d", got, want)
	}
}

func TestRecordActorPrunesExpiredCompletedAndFailedJobs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 26, 6, 30, 0, 0, time.UTC)
	actor := &RecordActor{
		config: config.Config{Record: config.RecordConfig{JobRetention: time.Hour}},
		jobs: map[string]recordJob{
			"old-completed": {
				Status:      recordStatusCompleted,
				CompletedAt: now.Add(-2 * time.Hour),
			},
			"old-failed": {
				Status:      recordStatusFailed,
				CompletedAt: now.Add(-2 * time.Hour),
			},
			"old-running": {
				Status:      recordStatusRunning,
				CompletedAt: now.Add(-2 * time.Hour),
			},
			"fresh-completed": {
				Status:      recordStatusCompleted,
				CompletedAt: now.Add(-30 * time.Minute),
			},
		},
	}

	actor.pruneExpiredJobs(now)

	if _, ok := actor.jobs["old-completed"]; ok {
		t.Fatal("old completed job was not pruned")
	}
	if _, ok := actor.jobs["old-failed"]; ok {
		t.Fatal("old failed job was not pruned")
	}
	if _, ok := actor.jobs["old-running"]; !ok {
		t.Fatal("running job should not be pruned")
	}
	if _, ok := actor.jobs["fresh-completed"]; !ok {
		t.Fatal("fresh completed job should not be pruned")
	}
}

type recordActorFakeLookup struct {
	info bodycamInfo
	err  error
}

func (l recordActorFakeLookup) LookupBodycamInfo(ctx context.Context, mac string) (bodycamInfo, error) {
	if l.err != nil {
		return bodycamInfo{}, l.err
	}
	return l.info, nil
}

func (l recordActorFakeLookup) Close(ctx context.Context) error {
	return nil
}

type recordActorFakeStore struct {
	mu             sync.Mutex
	ensureErr      error
	putErr         error
	ensureCalls    int
	putCalls       int
	ensuredBucket  string
	uploadedBucket string
	uploadedKey    string
	uploadedBody   string
}

func (s *recordActorFakeStore) EnsureBucket(ctx context.Context, bucket string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureCalls++
	s.ensuredBucket = bucket
	return s.ensureErr
}

func (s *recordActorFakeStore) PutObject(ctx context.Context, bucket string, objectKey string, reader io.Reader, size int64, contentType string) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCalls++
	s.uploadedBucket = bucket
	s.uploadedKey = objectKey
	s.uploadedBody = string(body)
	return s.putErr
}

func newTestRecordActor(root *protoactor.RootContext, go2rtcBaseURL string, lookup bodycamLookup, store objectStore) *RecordActor {
	return &RecordActor{
		config: config.Config{
			Go2RTC: config.Go2RTCConfig{
				BaseURL:        go2rtcBaseURL,
				RequestTimeout: time.Second,
			},
			Record: config.RecordConfig{
				MaxDuration:       time.Hour,
				JobRetention:      time.Hour,
				MaxConcurrentJobs: 3,
			},
		},
		logger:      zap.NewNop(),
		rootContext: root,
		httpClient:  &http.Client{Timeout: 2 * time.Second},
		lookup:      lookup,
		store:       store,
		jobs:        make(map[string]recordJob),
	}
}

func startTestRecordJob(t *testing.T, root *protoactor.RootContext, pid *protoactor.PID) *common.StartRecordResult {
	t.Helper()

	future := root.RequestFuture(pid, &common.StartRecordRequest{
		Type:        "UI",
		Mac:         "AB:CD:EF:DD:GG",
		CamID:       "TEST_P1000HDKFH",
		Duration:    10 * time.Second,
		RequestedAt: time.Date(2026, 4, 26, 6, 30, 0, 0, time.UTC),
	}, time.Second)
	response, err := future.Result()
	if err != nil {
		t.Fatalf("future result: %v", err)
	}

	result, ok := response.(*common.StartRecordResult)
	if !ok {
		t.Fatalf("unexpected response type: %T", response)
	}
	return result
}

func waitForRecordStatus(t *testing.T, root *protoactor.RootContext, pid *protoactor.PID, jobID string, wantStatus string) *common.RecordJobStatusResult {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	var last *common.RecordJobStatusResult
	for time.Now().Before(deadline) {
		future := root.RequestFuture(pid, &common.GetRecordJobRequest{JobID: jobID}, time.Second)
		response, err := future.Result()
		if err != nil {
			t.Fatalf("status future result: %v", err)
		}
		status, ok := response.(*common.RecordJobStatusResult)
		if !ok {
			t.Fatalf("unexpected status response type: %T", response)
		}
		last = status
		if status.Status == wantStatus {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}

	if last == nil {
		t.Fatalf("timed out waiting for status %q", wantStatus)
	}
	t.Fatalf("timed out waiting for status %q; last status=%q error=%q", wantStatus, last.Status, last.Error)
	return nil
}
