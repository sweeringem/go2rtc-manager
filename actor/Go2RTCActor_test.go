package actor

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/example/go2rtc-manager/config"
)

func TestGo2RTCActorListStreamNames(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "go2rtc.yml")
	content := []byte("streams:\n  camera_front: rtsp://front\n  camera_back: rtsp://back\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	actor := newTestGo2RTCActor(config.Config{
		Go2RTC: config.Go2RTCConfig{
			ConfigPath:     configPath,
			RequestTimeout: time.Second,
		},
	})

	streams, err := actor.listStreamNames()
	if err != nil {
		t.Fatalf("listStreamNames: %v", err)
	}

	expected := []string{"camera_back", "camera_front"}
	slices.Sort(streams)
	if !slices.Equal(streams, expected) {
		t.Fatalf("unexpected streams: got %v want %v", streams, expected)
	}
}

func TestGo2RTCActorHasProducer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
		wantErr    bool
	}{
		{
			name:       "producer exists",
			statusCode: http.StatusOK,
			body:       `{"camera_front":{"producers":[{"url":"rtsp://front"}]}}`,
			want:       true,
		},
		{
			name:       "producer missing",
			statusCode: http.StatusOK,
			body:       `{"camera_front":{"producers":[]}}`,
			want:       false,
		},
		{
			name:       "stream not found treated as dead",
			statusCode: http.StatusNotFound,
			body:       "",
			want:       false,
		},
		{
			name:       "unexpected status",
			statusCode: http.StatusInternalServerError,
			body:       "boom",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("unexpected method: %s", r.Method)
				}
				if got := r.URL.Query().Get("src"); got != "camera_front" {
					t.Fatalf("unexpected src query: %s", got)
				}

				w.WriteHeader(tt.statusCode)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer server.Close()

			actor := newTestGo2RTCActor(config.Config{
				Go2RTC: config.Go2RTCConfig{
					BaseURL:        server.URL,
					RequestTimeout: time.Second,
				},
			})

			got, err := actor.hasProducer("camera_front")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("hasProducer: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected producer state: got %v want %v", got, tt.want)
			}
		})
	}
}

func TestGo2RTCActorRemoveStreamUsesDeleteAPIAndBackup(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "go2rtc.yml")
	original := []byte("streams:\n  camera_front: rtsp://front\n")
	if err := os.WriteFile(configPath, original, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var gotMethod string
	var gotSrc string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotSrc = r.URL.Query().Get("src")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	actor := newTestGo2RTCActor(config.Config{
		Go2RTC: config.Go2RTCConfig{
			BaseURL:            server.URL,
			ConfigPath:         configPath,
			RequestTimeout:     time.Second,
			BackupBeforeChange: true,
		},
	})

	removed, err := actor.removeStream("camera_front")
	if err != nil {
		t.Fatalf("removeStream: %v", err)
	}
	if !removed {
		t.Fatal("expected stream to be removed")
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("unexpected method: got %s want %s", gotMethod, http.MethodDelete)
	}
	if gotSrc != "camera_front" {
		t.Fatalf("unexpected src query: got %s want %s", gotSrc, "camera_front")
	}

	backups, err := filepath.Glob(configPath + ".*.bak")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one backup file, got %d", len(backups))
	}

	backupContent, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupContent) != string(original) {
		t.Fatalf("unexpected backup content: got %q want %q", string(backupContent), string(original))
	}
}

func newTestGo2RTCActor(cfg config.Config) *Go2RTCActor {
	return NewGo2RTCActor(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
}
