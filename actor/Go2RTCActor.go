package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

type Go2RTCActor struct {
	config     config.Config
	logger     *zap.Logger
	httpClient *http.Client
}

func NewGo2RTCActor(cfg config.Config, logger *zap.Logger) *Go2RTCActor {
	return &Go2RTCActor{
		config: cfg,
		logger: logger.With(zap.String("actor", "Go2RTCActor")),
		httpClient: &http.Client{
			Timeout: cfg.Go2RTC.RequestTimeout,
		},
	}
}

func (a *Go2RTCActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.logger.Info("go2rtc actor started",
			zap.String("base_url", a.config.Go2RTC.BaseURL),
			zap.String("config_path", a.config.Go2RTC.ConfigPath),
		)
	case *common.FetchStreamList:
		streams, err := a.listStreamNames()
		if err != nil {
			a.logger.Error("failed to load go2rtc streams from config", zap.Error(err))
			ctx.Send(ctx.Parent(), &common.StreamListFetched{
				TriggeredAt: msg.TriggeredAt,
				RequestedBy: msg.RequestedBy,
				Error:       err.Error(),
			})
			return
		}

		ctx.Send(ctx.Parent(), &common.StreamListFetched{
			TriggeredAt: msg.TriggeredAt,
			Streams:     streams,
			RequestedBy: msg.RequestedBy,
		})
	case *common.CheckStreamHealth:
		hasProducer, err := a.hasProducer(msg.StreamName)
		if err != nil {
			a.logger.Error("failed to check stream producer",
				zap.String("stream", msg.StreamName),
				zap.Int("attempt", msg.Attempt),
				zap.Error(err),
			)
			ctx.Send(ctx.Parent(), &common.StreamHealthChecked{
				StreamName:   msg.StreamName,
				TriggeredAt:  msg.TriggeredAt,
				Attempt:      msg.Attempt,
				CheckedAt:    time.Now(),
				RequestedBy:  msg.RequestedBy,
				ConfirmAfter: msg.ConfirmAfter,
				Error:        err.Error(),
			})
			return
		}

		ctx.Send(ctx.Parent(), &common.StreamHealthChecked{
			StreamName:   msg.StreamName,
			TriggeredAt:  msg.TriggeredAt,
			Attempt:      msg.Attempt,
			HasProducer:  hasProducer,
			CheckedAt:    time.Now(),
			RequestedBy:  msg.RequestedBy,
			ConfirmAfter: msg.ConfirmAfter,
		})
	case *common.RemoveStream:
		removed, err := a.removeStream(msg.StreamName)
		if err != nil {
			a.logger.Error("failed to remove stream through go2rtc api", zap.String("stream", msg.StreamName), zap.Error(err))
			ctx.Send(ctx.Parent(), &common.StreamRemovalCompleted{
				StreamName:  msg.StreamName,
				TriggeredAt: msg.TriggeredAt,
				Error:       err.Error(),
			})
			return
		}
		if !removed {
			a.logger.Info("stream already absent in go2rtc api", zap.String("stream", msg.StreamName))
			ctx.Send(ctx.Parent(), &common.StreamRemovalCompleted{
				StreamName:  msg.StreamName,
				TriggeredAt: msg.TriggeredAt,
			})
			return
		}

		removedAt := time.Now()
		a.logger.Warn("dead stream removed through go2rtc api", zap.String("stream", msg.StreamName))
		ctx.Send(ctx.Parent(), &common.StreamRemoved{
			StreamName: msg.StreamName,
			RemovedAt:  removedAt,
		})
		ctx.Send(ctx.Parent(), &common.StreamRemovalCompleted{
			StreamName:  msg.StreamName,
			TriggeredAt: msg.TriggeredAt,
			Removed:     true,
			RemovedAt:   removedAt,
		})
	default:
	}
}

func (a *Go2RTCActor) listStreamNames() ([]string, error) {
	root, err := a.loadConfigDocument()
	if err != nil {
		return nil, err
	}

	streamsNode, err := findStreamsNode(root)
	if err != nil {
		return nil, err
	}

	streams := make([]string, 0, len(streamsNode.Content)/2)
	for i := 0; i < len(streamsNode.Content); i += 2 {
		streams = append(streams, streamsNode.Content[i].Value)
	}

	return streams, nil
}

func (a *Go2RTCActor) hasProducer(streamName string) (bool, error) {
	body, statusCode, err := a.doStreamsAPIRequest(context.Background(), http.MethodGet, streamName)
	if err != nil {
		return false, err
	}
	if statusCode == http.StatusNotFound {
		return false, nil
	}
	if statusCode < 200 || statusCode >= 300 {
		return false, fmt.Errorf("unexpected status %d: %s", statusCode, strings.TrimSpace(string(body)))
	}

	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return false, fmt.Errorf("decode response: %w", err)
	}

	if streamPayload, ok := lookupStreamPayload(payload, streamName); ok {
		return payloadHasProducer(streamPayload), nil
	}

	return payloadHasProducer(payload), nil
}

func (a *Go2RTCActor) removeStream(streamName string) (bool, error) {
	if a.config.Go2RTC.BackupBeforeChange {
		if err := a.createBackup(); err != nil {
			return false, err
		}
	}

	body, statusCode, err := a.doStreamsAPIRequest(context.Background(), http.MethodDelete, streamName)
	if err != nil {
		return false, err
	}
	if statusCode == http.StatusNotFound {
		return false, nil
	}
	if statusCode < 200 || statusCode >= 300 {
		return false, fmt.Errorf("unexpected status %d: %s", statusCode, strings.TrimSpace(string(body)))
	}

	return true, nil
}

func (a *Go2RTCActor) loadConfigDocument() (*yaml.Node, error) {
	raw, err := os.ReadFile(a.config.Go2RTC.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read go2rtc config: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parse go2rtc config yaml: %w", err)
	}
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("go2rtc config is empty")
	}

	return &root, nil
}

func (a *Go2RTCActor) createBackup() error {
	raw, err := os.ReadFile(a.config.Go2RTC.ConfigPath)
	if err != nil {
		return fmt.Errorf("read config for backup: %w", err)
	}

	backupPath := fmt.Sprintf("%s.%s.bak", a.config.Go2RTC.ConfigPath, time.Now().Format("20060102-150405"))
	if err := os.WriteFile(backupPath, raw, 0o644); err != nil {
		return fmt.Errorf("write backup file: %w", err)
	}

	return nil
}

func (a *Go2RTCActor) doStreamsAPIRequest(ctx context.Context, method string, streamName string) ([]byte, int, error) {
	baseURL, err := url.Parse(strings.TrimRight(a.config.Go2RTC.BaseURL, "/"))
	if err != nil {
		return nil, 0, fmt.Errorf("parse base url: %w", err)
	}

	requestURL := *baseURL
	requestURL.Path = path.Join(baseURL.Path, "/api/streams")
	query := requestURL.Query()
	query.Set("src", streamName)
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, requestURL.String(), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	if a.config.Go2RTC.Username != "" {
		req.SetBasicAuth(a.config.Go2RTC.Username, a.config.Go2RTC.Password)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("call go2rtc api: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return body, resp.StatusCode, nil
}

func findStreamsNode(root *yaml.Node) (*yaml.Node, error) {
	document := root
	if document.Kind == yaml.DocumentNode && len(document.Content) > 0 {
		document = document.Content[0]
	}
	if document.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("go2rtc config root must be a mapping")
	}

	for i := 0; i < len(document.Content); i += 2 {
		keyNode := document.Content[i]
		valueNode := document.Content[i+1]
		if keyNode.Value != "streams" {
			continue
		}
		if valueNode.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("go2rtc streams section must be a mapping")
		}
		return valueNode, nil
	}

	return nil, fmt.Errorf("streams section not found in go2rtc config")
}

func lookupStreamPayload(payload any, streamName string) (any, bool) {
	switch typed := payload.(type) {
	case map[string]any:
		if stream, ok := typed[streamName]; ok {
			return stream, true
		}
		if streams, ok := typed["streams"].(map[string]any); ok {
			if stream, ok := streams[streamName]; ok {
				return stream, true
			}
		}
	}

	return nil, false
}

func payloadHasProducer(payload any) bool {
	switch typed := payload.(type) {
	case map[string]any:
		if producers, ok := typed["producers"]; ok {
			switch producersTyped := producers.(type) {
			case []any:
				return len(producersTyped) > 0
			case nil:
				return false
			default:
				return true
			}
		}
		for _, nested := range typed {
			if payloadHasProducer(nested) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if payloadHasProducer(item) {
				return true
			}
		}
	}

	return false
}
