package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type Server struct {
	cfg        config.Config
	logger     *slog.Logger
	root       *protoactor.RootContext
	masterPID  *protoactor.PID
	httpServer *http.Server
}

type captureSnapshotHTTPPayload struct {
	CamID string `json:"cam_id"`
}

type captureSnapshotHTTPResponse struct {
	CamID     string `json:"cam_id,omitempty"`
	SavedPath string `json:"saved_path,omitempty"`
	Error     string `json:"error,omitempty"`
}

type startRecordHTTPPayload struct {
	Type     string `json:"TYPE"`
	Mac      string `json:"mac"`
	CamID    string `json:"cam_id"`
	Duration string `json:"duration"`
}

type recordHTTPResponse struct {
	JobID       string `json:"job_id,omitempty"`
	Status      string `json:"status,omitempty"`
	Type        string `json:"TYPE,omitempty"`
	Mac         string `json:"mac,omitempty"`
	CamID       string `json:"cam_id,omitempty"`
	Duration    string `json:"duration,omitempty"`
	Bucket      string `json:"bucket,omitempty"`
	ObjectKey   string `json:"object_key,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	Error       string `json:"error,omitempty"`
}

func New(cfg config.Config, logger *slog.Logger, root *protoactor.RootContext, masterPID *protoactor.PID) *Server {
	mux := http.NewServeMux()
	server := &Server{
		cfg:       cfg,
		logger:    logger.With("component", "httpserver"),
		root:      root,
		masterPID: masterPID,
	}
	mux.HandleFunc("/snapshots", server.handleCaptureSnapshot)
	mux.HandleFunc("/record", server.handleStartRecord)
	mux.HandleFunc("/record/", server.handleGetRecordJob)
	server.httpServer = &http.Server{
		Addr:         cfg.HTTP.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
		IdleTimeout:  cfg.HTTP.IdleTimeout,
	}
	return server
}

func (s *Server) Start() error {
	s.logger.Info("http server started", "addr", s.cfg.HTTP.Addr)
	err := s.httpServer.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) Shutdown() error {
	return s.httpServer.Shutdown(context.Background())
}

func (s *Server) handleCaptureSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, captureSnapshotHTTPResponse{Error: "method not allowed"})
		return
	}

	var payload captureSnapshotHTTPPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, captureSnapshotHTTPResponse{Error: "invalid json body"})
		return
	}
	if payload.CamID == "" {
		writeJSON(w, http.StatusBadRequest, captureSnapshotHTTPResponse{Error: "cam_id is required"})
		return
	}

	future := s.root.RequestFuture(s.masterPID, &common.CaptureSnapshotRequest{
		CamID:       payload.CamID,
		RequestedAt: time.Now(),
	}, s.cfg.HTTP.WriteTimeout)
	response, err := future.Result()
	if err != nil {
		writeJSON(w, http.StatusGatewayTimeout, captureSnapshotHTTPResponse{CamID: payload.CamID, Error: err.Error()})
		return
	}

	result, ok := response.(*common.CaptureSnapshotResult)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, captureSnapshotHTTPResponse{CamID: payload.CamID, Error: "unexpected actor response"})
		return
	}
	if result.Error != "" {
		statusCode := http.StatusInternalServerError
		switch result.StatusCode {
		case http.StatusNotFound:
			statusCode = http.StatusNotFound
		case http.StatusBadGateway:
			statusCode = http.StatusBadGateway
		}
		writeJSON(w, statusCode, captureSnapshotHTTPResponse{CamID: result.CamID, Error: result.Error})
		return
	}

	writeJSON(w, http.StatusCreated, captureSnapshotHTTPResponse{CamID: result.CamID, SavedPath: result.SavedPath})
}

func (s *Server) handleStartRecord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, recordHTTPResponse{Error: "method not allowed"})
		return
	}

	var payload startRecordHTTPPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "invalid json body"})
		return
	}

	recordType := strings.ToUpper(strings.TrimSpace(payload.Type))
	mac := strings.TrimSpace(payload.Mac)
	camID := strings.TrimSpace(payload.CamID)
	durationText := strings.TrimSpace(payload.Duration)
	if recordType == "" {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "TYPE is required"})
		return
	}
	if recordType != "UI" && recordType != "BODYCAM" {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "TYPE must be UI or BODYCAM"})
		return
	}
	if mac == "" {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "mac is required"})
		return
	}
	if camID == "" {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "cam_id is required"})
		return
	}
	if durationText == "" {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "duration is required"})
		return
	}

	duration, err := time.ParseDuration(durationText)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "duration is invalid"})
		return
	}
	if duration <= 0 {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "duration must be greater than zero"})
		return
	}
	if duration > s.cfg.Record.MaxDuration {
		writeJSON(w, http.StatusBadRequest, recordHTTPResponse{Error: "duration exceeds record.max_duration"})
		return
	}

	future := s.root.RequestFuture(s.masterPID, &common.StartRecordRequest{
		Type:        recordType,
		Mac:         mac,
		CamID:       camID,
		Duration:    duration,
		RequestedAt: time.Now(),
	}, s.cfg.HTTP.WriteTimeout)
	response, err := future.Result()
	if err != nil {
		writeJSON(w, http.StatusGatewayTimeout, recordHTTPResponse{Mac: mac, CamID: camID, Error: err.Error()})
		return
	}

	result, ok := response.(*common.StartRecordResult)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, recordHTTPResponse{Mac: mac, CamID: camID, Error: "unexpected actor response"})
		return
	}
	if result.Error != "" {
		statusCode := result.StatusCode
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}
		writeJSON(w, statusCode, recordHTTPResponse{Type: result.Type, Mac: result.Mac, CamID: result.CamID, Duration: result.Duration.String(), Error: result.Error})
		return
	}

	writeJSON(w, http.StatusAccepted, recordHTTPResponse{
		JobID:    result.JobID,
		Status:   result.Status,
		Type:     result.Type,
		Mac:      result.Mac,
		CamID:    result.CamID,
		Duration: result.Duration.String(),
	})
}

func (s *Server) handleGetRecordJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, recordHTTPResponse{Error: "method not allowed"})
		return
	}

	jobID := strings.TrimPrefix(r.URL.Path, "/record/")
	if jobID == "" || strings.Contains(jobID, "/") {
		writeJSON(w, http.StatusNotFound, recordHTTPResponse{Error: "record job not found"})
		return
	}

	future := s.root.RequestFuture(s.masterPID, &common.GetRecordJobRequest{JobID: jobID}, s.cfg.HTTP.WriteTimeout)
	response, err := future.Result()
	if err != nil {
		writeJSON(w, http.StatusGatewayTimeout, recordHTTPResponse{JobID: jobID, Error: err.Error()})
		return
	}

	result, ok := response.(*common.RecordJobStatusResult)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, recordHTTPResponse{JobID: jobID, Error: "unexpected actor response"})
		return
	}
	statusCode := result.StatusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	writeJSON(w, statusCode, recordStatusHTTPResponse(result))
}

func recordStatusHTTPResponse(result *common.RecordJobStatusResult) recordHTTPResponse {
	response := recordHTTPResponse{
		JobID:       result.JobID,
		Status:      result.Status,
		Type:        result.Type,
		Mac:         result.Mac,
		CamID:       result.CamID,
		Bucket:      result.Bucket,
		ObjectKey:   result.ObjectKey,
		ContentType: result.ContentType,
		Error:       result.Error,
	}
	if result.Duration > 0 {
		response.Duration = result.Duration.String()
	}
	if !result.StartedAt.IsZero() {
		response.StartedAt = result.StartedAt.UTC().Format(time.RFC3339)
	}
	if !result.CompletedAt.IsZero() {
		response.CompletedAt = result.CompletedAt.UTC().Format(time.RFC3339)
	}
	return response
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
