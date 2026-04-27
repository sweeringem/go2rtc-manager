package actor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	protoactor "github.com/asynkron/protoactor-go/actor"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"

	"github.com/example/go2rtc-manager/common"
	"github.com/example/go2rtc-manager/config"
)

const (
	recordStatusAccepted  = "accepted"
	recordStatusRunning   = "running"
	recordStatusCompleted = "completed"
	recordStatusFailed    = "failed"

	recordContentType = "video/mp4"
)

var validBucketNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

type bodycamInfo struct {
	Process string `bson:"process"`
	Site    string `bson:"site"`
	Group   string `bson:"group"`
}

type bodycamLookup interface {
	LookupBodycamInfo(ctx context.Context, mac string) (bodycamInfo, error)
	Close(ctx context.Context) error
}

type objectStore interface {
	EnsureBucket(ctx context.Context, bucket string) error
	PutObject(ctx context.Context, bucket string, objectKey string, reader io.Reader, size int64, contentType string) error
}

type RecordActor struct {
	config      config.Config
	logger      *zap.Logger
	rootContext *protoactor.RootContext
	httpClient  *http.Client
	lookup      bodycamLookup
	store       objectStore
	jobs        map[string]recordJob
	initError   string
}

type recordJob struct {
	JobID       string
	Status      string
	Type        string
	Mac         string
	CamID       string
	Duration    time.Duration
	Bucket      string
	ObjectKey   string
	ContentType string
	StartedAt   time.Time
	CompletedAt time.Time
	Error       string
}

type recordJobRunning struct {
	JobID     string
	StartedAt time.Time
}

type recordJobCompleted struct {
	JobID       string
	Bucket      string
	ObjectKey   string
	ContentType string
	CompletedAt time.Time
}

type recordJobFailed struct {
	JobID       string
	Error       string
	CompletedAt time.Time
}

func NewRecordActor(cfg config.Config, logger *zap.Logger, root *protoactor.RootContext) *RecordActor {
	actor := &RecordActor{
		config:      cfg,
		logger:      logger.With(zap.String("actor", "RecordActor")),
		rootContext: root,
		httpClient: &http.Client{
			Timeout: cfg.Record.MaxDuration + cfg.Go2RTC.RequestTimeout,
		},
		jobs: make(map[string]recordJob),
	}

	lookup, err := newMongoBodycamLookup(cfg)
	if err != nil {
		actor.initError = err.Error()
		return actor
	}
	actor.lookup = lookup

	store, err := newMinIOObjectStore(cfg)
	if err != nil {
		actor.initError = err.Error()
		return actor
	}
	actor.store = store

	return actor
}

func (a *RecordActor) Receive(ctx protoactor.Context) {
	switch msg := ctx.Message().(type) {
	case *protoactor.Started:
		a.logger.Info("record actor started",
			zap.Duration("max_duration", a.config.Record.MaxDuration),
			zap.Duration("job_retention", a.config.Record.JobRetention),
		)
	case *protoactor.Stopping:
		if a.lookup != nil {
			if err := a.lookup.Close(context.Background()); err != nil {
				a.logger.Error("failed to close mongodb client", zap.Error(err))
			}
		}
	case *common.StartRecordRequest:
		a.pruneExpiredJobs(time.Now())
		a.handleStartRecord(ctx, msg)
	case *common.GetRecordJobRequest:
		a.pruneExpiredJobs(time.Now())
		a.handleGetRecordJob(ctx, msg)
	case *recordJobRunning:
		if job, ok := a.jobs[msg.JobID]; ok {
			job.Status = recordStatusRunning
			job.StartedAt = msg.StartedAt
			a.jobs[msg.JobID] = job
		}
	case *recordJobCompleted:
		if job, ok := a.jobs[msg.JobID]; ok {
			job.Status = recordStatusCompleted
			job.Bucket = msg.Bucket
			job.ObjectKey = msg.ObjectKey
			job.ContentType = msg.ContentType
			job.CompletedAt = msg.CompletedAt
			a.jobs[msg.JobID] = job
		}
	case *recordJobFailed:
		if job, ok := a.jobs[msg.JobID]; ok {
			job.Status = recordStatusFailed
			job.Error = msg.Error
			job.CompletedAt = msg.CompletedAt
			a.jobs[msg.JobID] = job
		}
	default:
	}
}

func (a *RecordActor) handleStartRecord(ctx protoactor.Context, msg *common.StartRecordRequest) {
	if a.initError != "" {
		ctx.Respond(&common.StartRecordResult{
			Type:       msg.Type,
			Mac:        msg.Mac,
			CamID:      msg.CamID,
			Duration:   msg.Duration,
			StatusCode: http.StatusInternalServerError,
			Error:      a.initError,
		})
		return
	}
	if a.activeRecordJobs() >= a.config.Record.MaxConcurrentJobs {
		ctx.Respond(&common.StartRecordResult{
			Type:       msg.Type,
			Mac:        msg.Mac,
			CamID:      msg.CamID,
			Duration:   msg.Duration,
			StatusCode: http.StatusTooManyRequests,
			Error:      "too many active record jobs",
		})
		return
	}

	now := msg.RequestedAt
	if now.IsZero() {
		now = time.Now()
	}
	jobID, err := newRecordJobID(now)
	if err != nil {
		ctx.Respond(&common.StartRecordResult{
			Type:       msg.Type,
			Mac:        msg.Mac,
			CamID:      msg.CamID,
			Duration:   msg.Duration,
			StatusCode: http.StatusInternalServerError,
			Error:      err.Error(),
		})
		return
	}

	job := recordJob{
		JobID:    jobID,
		Status:   recordStatusAccepted,
		Type:     msg.Type,
		Mac:      msg.Mac,
		CamID:    msg.CamID,
		Duration: msg.Duration,
	}
	a.jobs[jobID] = job

	self := ctx.Self()
	go a.executeRecord(self, job)

	ctx.Respond(&common.StartRecordResult{
		JobID:      job.JobID,
		Status:     job.Status,
		Type:       job.Type,
		Mac:        job.Mac,
		CamID:      job.CamID,
		Duration:   job.Duration,
		StatusCode: http.StatusAccepted,
	})
}

func (a *RecordActor) handleGetRecordJob(ctx protoactor.Context, msg *common.GetRecordJobRequest) {
	job, ok := a.jobs[msg.JobID]
	if !ok {
		ctx.Respond(&common.RecordJobStatusResult{
			JobID:      msg.JobID,
			StatusCode: http.StatusNotFound,
			Error:      "record job not found",
		})
		return
	}

	ctx.Respond(recordJobToStatusResult(job))
}

func (a *RecordActor) executeRecord(self *protoactor.PID, job recordJob) {
	startedAt := time.Now().UTC()
	a.rootContext.Send(self, &recordJobRunning{JobID: job.JobID, StartedAt: startedAt})

	ctx, cancel := context.WithTimeout(context.Background(), job.Duration+a.config.Go2RTC.RequestTimeout+30*time.Second)
	defer cancel()

	info, err := a.lookup.LookupBodycamInfo(ctx, job.Mac)
	if err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}

	bucket, err := normalizeBucketName(info.Process)
	if err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}

	objectKey, filename, err := buildRecordObjectKey(info.Site, info.Group, job.CamID, job.Type, startedAt)
	if err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}

	resp, err := a.fetchRecordStream(ctx, job.CamID, job.Duration, filename)
	if err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}
	defer resp.Body.Close()

	if err := a.store.EnsureBucket(ctx, bucket); err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}

	size := resp.ContentLength
	if size < 0 {
		size = -1
	}
	if err := a.store.PutObject(ctx, bucket, objectKey, resp.Body, size, recordContentType); err != nil {
		a.failRecordJob(self, job.JobID, err)
		return
	}

	a.rootContext.Send(self, &recordJobCompleted{
		JobID:       job.JobID,
		Bucket:      bucket,
		ObjectKey:   objectKey,
		ContentType: recordContentType,
		CompletedAt: time.Now().UTC(),
	})
}

func (a *RecordActor) failRecordJob(self *protoactor.PID, jobID string, err error) {
	a.logger.Error("record job failed", zap.String("job_id", jobID), zap.Error(err))
	a.rootContext.Send(self, &recordJobFailed{
		JobID:       jobID,
		Error:       err.Error(),
		CompletedAt: time.Now().UTC(),
	})
}

func (a *RecordActor) fetchRecordStream(ctx context.Context, camID string, duration time.Duration, filename string) (*http.Response, error) {
	baseURL, err := url.Parse(strings.TrimRight(a.config.Go2RTC.BaseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}

	requestURL := *baseURL
	requestURL.Path = path.Join(baseURL.Path, "/api/stream.mp4")
	query := requestURL.Query()
	query.Set("src", camID)
	query.Set("duration", fmt.Sprintf("%d", int(math.Ceil(duration.Seconds()))))
	query.Set("filename", filename)
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build record request: %w", err)
	}
	if a.config.Go2RTC.Username != "" {
		req.SetBasicAuth(a.config.Go2RTC.Username, a.config.Go2RTC.Password)
	}

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call go2rtc record api: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read record error response: %w", readErr)
		}
		return nil, fmt.Errorf("unexpected go2rtc record status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return resp, nil
}

func (a *RecordActor) pruneExpiredJobs(now time.Time) {
	for jobID, job := range a.jobs {
		if job.Status != recordStatusCompleted && job.Status != recordStatusFailed {
			continue
		}
		if job.CompletedAt.IsZero() {
			continue
		}
		if now.Sub(job.CompletedAt) > a.config.Record.JobRetention {
			delete(a.jobs, jobID)
		}
	}
}

func (a *RecordActor) activeRecordJobs() int {
	active := 0
	for _, job := range a.jobs {
		if job.Status == recordStatusAccepted || job.Status == recordStatusRunning {
			active++
		}
	}
	return active
}

func recordJobToStatusResult(job recordJob) *common.RecordJobStatusResult {
	return &common.RecordJobStatusResult{
		JobID:       job.JobID,
		Status:      job.Status,
		Type:        job.Type,
		Mac:         job.Mac,
		CamID:       job.CamID,
		Duration:    job.Duration,
		Bucket:      job.Bucket,
		ObjectKey:   job.ObjectKey,
		ContentType: job.ContentType,
		StartedAt:   job.StartedAt,
		CompletedAt: job.CompletedAt,
		Error:       job.Error,
		StatusCode:  http.StatusOK,
	}
}

func newRecordJobID(now time.Time) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate job id: %w", err)
	}
	return fmt.Sprintf("record_%s_%s", now.UTC().Format("20060102T150405Z"), hex.EncodeToString(suffix[:])), nil
}

func normalizeBucketName(process string) (string, error) {
	bucket := strings.TrimSpace(process)
	bucket = strings.ToLower(bucket)
	bucket = strings.ReplaceAll(bucket, "_", "-")
	if !isValidBucketName(bucket) {
		return "", fmt.Errorf("invalid bucket name from process: %s", process)
	}
	return bucket, nil
}

func isValidBucketName(bucket string) bool {
	if !validBucketNamePattern.MatchString(bucket) {
		return false
	}
	if strings.Contains(bucket, "..") || strings.Contains(bucket, ".-") || strings.Contains(bucket, "-.") {
		return false
	}
	return net.ParseIP(bucket) == nil
}

func buildRecordObjectKey(site string, group string, camID string, recordType string, startedAt time.Time) (string, string, error) {
	siteSegment, err := sanitizeObjectKeySegment(site, "site")
	if err != nil {
		return "", "", err
	}
	groupSegment, err := sanitizeObjectKeySegment(group, "group")
	if err != nil {
		return "", "", err
	}
	camSegment, err := sanitizeObjectKeySegment(camID, "cam_id")
	if err != nil {
		return "", "", err
	}

	filename := fmt.Sprintf("%s_%s_%s.mp4", camSegment, recordType, startedAt.UTC().Format("20060102T150405Z"))
	return path.Join(siteSegment, groupSegment, filename), filename, nil
}

func sanitizeObjectKeySegment(value string, fieldName string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required for record object key", fieldName)
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_")
	return replacer.Replace(value), nil
}

type mongoBodycamLookup struct {
	client     *mongo.Client
	collection *mongo.Collection
}

func newMongoBodycamLookup(cfg config.Config) (*mongoBodycamLookup, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoDB.URI))
	if err != nil {
		return nil, fmt.Errorf("connect mongodb: %w", err)
	}
	return &mongoBodycamLookup{
		client:     client,
		collection: client.Database(cfg.MongoDB.Database).Collection(cfg.MongoDB.Collection),
	}, nil
}

func (l *mongoBodycamLookup) LookupBodycamInfo(ctx context.Context, mac string) (bodycamInfo, error) {
	var info bodycamInfo
	if err := l.collection.FindOne(ctx, bson.M{"mac": mac}).Decode(&info); err != nil {
		if err == mongo.ErrNoDocuments {
			return bodycamInfo{}, fmt.Errorf("mongo bodycam info not found")
		}
		return bodycamInfo{}, fmt.Errorf("find bodycam info: %w", err)
	}
	if info.Process == "" || info.Site == "" || info.Group == "" {
		return bodycamInfo{}, fmt.Errorf("mongo bodycam info missing process, site, or group")
	}
	return info, nil
}

func (l *mongoBodycamLookup) Close(ctx context.Context) error {
	return l.client.Disconnect(ctx)
}

type minIOObjectStore struct {
	client *minio.Client
}

func newMinIOObjectStore(cfg config.Config) (*minIOObjectStore, error) {
	client, err := minio.New(cfg.MinIO.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, ""),
		Secure: cfg.MinIO.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	return &minIOObjectStore{client: client}, nil
}

func (s *minIOObjectStore) EnsureBucket(ctx context.Context, bucket string) error {
	exists, err := s.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check minio bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create minio bucket: %w", err)
	}
	return nil
}

func (s *minIOObjectStore) PutObject(ctx context.Context, bucket string, objectKey string, reader io.Reader, size int64, contentType string) error {
	if _, err := s.client.PutObject(ctx, bucket, objectKey, reader, size, minio.PutObjectOptions{ContentType: contentType}); err != nil {
		return fmt.Errorf("put minio object: %w", err)
	}
	return nil
}
