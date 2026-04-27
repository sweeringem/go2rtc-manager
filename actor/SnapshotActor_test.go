package actor

import (
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/go2rtc-manager/config"
)

func TestSnapshotActorCaptureAndSave(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Path; got != "/api/frame.jpeg" {
			t.Fatalf("unexpected path: %s", got)
		}
		if got := r.URL.Query().Get("src"); got != "TEST_P1000HDKFH" {
			t.Fatalf("unexpected src query: %s", got)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte("jpeg-binary"))
	}))
	defer server.Close()

	storageDir := t.TempDir()
	actor := NewSnapshotActor(config.Config{
		Go2RTC: config.Go2RTCConfig{
			BaseURL:        server.URL,
			RequestTimeout: time.Second,
		},
		Snapshot: config.SnapshotConfig{
			StorageDir: storageDir,
		},
	}, zap.NewNop())

	savedPath, statusCode, err := actor.captureAndSave("TEST_P1000HDKFH")
	if err != nil {
		t.Fatalf("captureAndSave: %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", statusCode, http.StatusOK)
	}

	expectedPath := filepath.Join(storageDir, "TEST_P1000HDKFH.jpg")
	if savedPath != expectedPath {
		t.Fatalf("unexpected saved path: got %s want %s", savedPath, expectedPath)
	}

	content, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if string(content) != "jpeg-binary" {
		t.Fatalf("unexpected snapshot content: got %q want %q", string(content), "jpeg-binary")
	}
}

func TestSnapshotActorCaptureAndSaveNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	actor := NewSnapshotActor(config.Config{
		Go2RTC: config.Go2RTCConfig{
			BaseURL:        server.URL,
			RequestTimeout: time.Second,
		},
		Snapshot: config.SnapshotConfig{
			StorageDir: t.TempDir(),
		},
	}, zap.NewNop())

	_, statusCode, err := actor.captureAndSave("TEST_P1000HDKFH")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if statusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code: got %d want %d", statusCode, http.StatusNotFound)
	}
}
