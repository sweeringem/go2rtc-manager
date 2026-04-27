package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type snapshotResponderActor struct{}

func (a *snapshotResponderActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *common.CaptureSnapshotRequest:
		ctx.Respond(&common.CaptureSnapshotResult{
			CamID:       msg.CamID,
			RequestedAt: msg.RequestedAt,
			SavedPath:   "storage/TEST_P1000HDKFH.jpg",
			StatusCode:  http.StatusOK,
		})
	case *common.StartRecordRequest:
		ctx.Respond(&common.StartRecordResult{
			JobID:      "record_20260426T063000Z_a1b2c3d4",
			Status:     "accepted",
			Type:       msg.Type,
			Mac:        msg.Mac,
			CamID:      msg.CamID,
			Duration:   msg.Duration,
			StatusCode: http.StatusAccepted,
		})
	case *common.GetRecordJobRequest:
		ctx.Respond(&common.RecordJobStatusResult{
			JobID:       msg.JobID,
			Status:      "completed",
			Type:        "UI",
			Mac:         "AB:CD:EF:DD:GG",
			CamID:       "TEST_P1000HDKFH",
			Duration:    10 * time.Second,
			Bucket:      "bodycam-prod",
			ObjectKey:   "site-a/group-1/TEST_P1000HDKFH_UI_20260426T063000Z.mp4",
			ContentType: "video/mp4",
			StartedAt:   time.Date(2026, 4, 26, 6, 30, 0, 0, time.UTC),
			CompletedAt: time.Date(2026, 4, 26, 6, 30, 10, 0, time.UTC),
			StatusCode:  http.StatusOK,
		})
	}
}

func TestHandleCaptureSnapshot(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
	}, zap.NewNop(), system.Root, masterPID)

	body, err := json.Marshal(map[string]string{"cam_id": "TEST_P1000HDKFH"})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/snapshots", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleCaptureSnapshot(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusCreated)
	}

	var response captureSnapshotHTTPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.CamID != "TEST_P1000HDKFH" {
		t.Fatalf("unexpected cam_id: got %s", response.CamID)
	}
	if response.SavedPath != "storage/TEST_P1000HDKFH.jpg" {
		t.Fatalf("unexpected saved_path: got %s", response.SavedPath)
	}
}

func TestHandleCaptureSnapshotBadRequest(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodPost, "/snapshots", bytes.NewReader([]byte(`{"cam_id":""}`)))
	w := httptest.NewRecorder()

	server.handleCaptureSnapshot(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleStartRecord(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))
	core, logs := observer.New(zap.InfoLevel)

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			MaxDuration: time.Hour,
			AllowedIPs:  []string{"192.0.2.10"},
		},
	}, zap.New(core), system.Root, masterPID)

	body, err := json.Marshal(map[string]string{
		"TYPE":     "UI",
		"mac":      "AB:CD:EF:DD:GG",
		"cam_id":   "TEST_P1000HDKFH",
		"duration": "10s",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()

	server.handleStartRecord(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusAccepted)
	}

	var response recordHTTPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.JobID != "record_20260426T063000Z_a1b2c3d4" {
		t.Fatalf("unexpected job_id: got %s", response.JobID)
	}
	if response.Type != "UI" {
		t.Fatalf("unexpected TYPE: got %s", response.Type)
	}
	if logs.FilterMessage("record request accepted").Len() != 1 {
		t.Fatalf("expected record request accepted log, got %d", logs.FilterMessage("record request accepted").Len())
	}
}

func TestHandleStartRecordBadRequest(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			MaxDuration: time.Hour,
			AllowedIPs:  []string{"192.0.2.10"},
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader([]byte(`{"TYPE":"INVALID","mac":"AB:CD:EF:DD:GG","cam_id":"TEST_P1000HDKFH","duration":"10s"}`)))
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()

	server.handleStartRecord(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetRecordJob(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			AllowedIPs: []string{"192.0.2.10"},
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodGet, "/record/record_20260426T063000Z_a1b2c3d4", nil)
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()

	server.handleGetRecordJob(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusOK)
	}

	var response recordHTTPResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.ObjectKey != "site-a/group-1/TEST_P1000HDKFH_UI_20260426T063000Z.mp4" {
		t.Fatalf("unexpected object_key: got %s", response.ObjectKey)
	}
	if response.StartedAt != "2026-04-26T06:30:00Z" {
		t.Fatalf("unexpected started_at: got %s", response.StartedAt)
	}
}

func TestHandleStartRecordForbiddenByRemoteAddr(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			MaxDuration: time.Hour,
			AllowedIPs:  []string{"192.0.2.10"},
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader([]byte(`{"TYPE":"UI","mac":"AB:CD:EF:DD:GG","cam_id":"TEST_P1000HDKFH","duration":"10s"}`)))
	req.RemoteAddr = "192.0.2.11:12345"
	w := httptest.NewRecorder()

	server.handleStartRecord(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleGetRecordJobForbiddenByRemoteAddr(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			AllowedIPs: []string{"192.0.2.10"},
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodGet, "/record/record_20260426T063000Z_a1b2c3d4", nil)
	req.RemoteAddr = "192.0.2.11:12345"
	w := httptest.NewRecorder()

	server.handleGetRecordJob(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusForbidden)
	}
}

func TestHandleStartRecordAllowsCIDR(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			MaxDuration: time.Hour,
			AllowedIPs:  []string{"192.0.2.0/24"},
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader([]byte(`{"TYPE":"UI","mac":"AB:CD:EF:DD:GG","cam_id":"TEST_P1000HDKFH","duration":"10s"}`)))
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()

	server.handleStartRecord(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusAccepted)
	}
}

func TestHandleStartRecordForbiddenWhenAllowedIPsEmpty(t *testing.T) {
	t.Parallel()

	system := protoactor.NewActorSystem()
	masterPID := system.Root.Spawn(protoactor.PropsFromProducer(func() protoactor.Actor {
		return &snapshotResponderActor{}
	}))

	server := New(config.Config{
		HTTP: config.HTTPConfig{
			Addr:         ":7181",
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  30 * time.Second,
		},
		Record: config.RecordConfig{
			MaxDuration: time.Hour,
		},
	}, zap.NewNop(), system.Root, masterPID)

	req := httptest.NewRequest(http.MethodPost, "/record", bytes.NewReader([]byte(`{"TYPE":"UI","mac":"AB:CD:EF:DD:GG","cam_id":"TEST_P1000HDKFH","duration":"10s"}`)))
	req.RemoteAddr = "192.0.2.10:12345"
	w := httptest.NewRecorder()

	server.handleStartRecord(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusForbidden)
	}
}
