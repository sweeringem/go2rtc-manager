package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
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

func New(cfg config.Config, logger *slog.Logger, root *protoactor.RootContext, masterPID *protoactor.PID) *Server {
	mux := http.NewServeMux()
	server := &Server{
		cfg:       cfg,
		logger:    logger.With("component", "httpserver"),
		root:      root,
		masterPID: masterPID,
	}
	mux.HandleFunc("/snapshots", server.handleCaptureSnapshot)
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

func writeJSON(w http.ResponseWriter, statusCode int, payload captureSnapshotHTTPResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
