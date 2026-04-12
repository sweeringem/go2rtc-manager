package actor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type SnapshotActor struct {
	config     config.Config
	logger     *slog.Logger
	httpClient *http.Client
}

func NewSnapshotActor(cfg config.Config, logger *slog.Logger) *SnapshotActor {
	return &SnapshotActor{
		config: cfg,
		logger: logger.With("actor", "SnapshotActor"),
		httpClient: &http.Client{
			Timeout: cfg.Go2RTC.RequestTimeout,
		},
	}
}

func (a *SnapshotActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.logger.Info("snapshot actor started", "storage_dir", a.config.Snapshot.StorageDir)
	case *common.CaptureSnapshotRequest:
		savedPath, statusCode, err := a.captureAndSave(msg.CamID)
		result := &common.CaptureSnapshotResult{
			CamID:       msg.CamID,
			RequestedAt: msg.RequestedAt,
			SavedPath:   savedPath,
			StatusCode:  statusCode,
		}
		if err != nil {
			result.Error = err.Error()
			a.logger.Error("snapshot capture failed", "cam_id", msg.CamID, "status_code", statusCode, "error", err)
		} else {
			a.logger.Info("snapshot captured", "cam_id", msg.CamID, "saved_path", savedPath)
		}
		ctx.Respond(result)
	default:
	}
}

func (a *SnapshotActor) captureAndSave(camID string) (string, int, error) {
	body, _, statusCode, err := a.fetchSnapshot(camID)
	if err != nil {
		return "", statusCode, err
	}

	if err := os.MkdirAll(a.config.Snapshot.StorageDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("create storage dir: %w", err)
	}

	filename := sanitizeFileName(camID) + ".jpg"
	fullPath := filepath.Join(a.config.Snapshot.StorageDir, filename)
	if err := os.WriteFile(fullPath, body, 0o644); err != nil {
		return "", 0, fmt.Errorf("write snapshot file: %w", err)
	}

	return fullPath, statusCode, nil
}

func (a *SnapshotActor) fetchSnapshot(camID string) ([]byte, string, int, error) {
	baseURL, err := url.Parse(strings.TrimRight(a.config.Go2RTC.BaseURL, "/"))
	if err != nil {
		return nil, "", 0, fmt.Errorf("parse base url: %w", err)
	}

	requestURL := *baseURL
	requestURL.Path = path.Join(baseURL.Path, "/api/frame.jpeg")
	query := requestURL.Query()
	query.Set("src", camID)
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, "", 0, fmt.Errorf("build request: %w", err)
	}
	if a.config.Go2RTC.Username != "" {
		req.SetBasicAuth(a.config.Go2RTC.Username, a.config.Go2RTC.Password)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, "", 0, fmt.Errorf("call go2rtc frame api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", resp.StatusCode, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, resp.Header.Get("Content-Type"), resp.StatusCode, fmt.Errorf("stream not found: %s", camID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header.Get("Content-Type"), resp.StatusCode, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "" && !strings.HasPrefix(contentType, "image/") {
		return nil, contentType, resp.StatusCode, fmt.Errorf("unexpected content type: %s", contentType)
	}

	return body, contentType, resp.StatusCode, nil
}

func sanitizeFileName(name string) string {
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	clean := strings.TrimSpace(replacer.Replace(name))
	if clean == "" {
		return "snapshot"
	}
	return clean
}
