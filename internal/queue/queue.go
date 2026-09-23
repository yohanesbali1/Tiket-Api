package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	activeKey      = "vq:active"
	waitingKey     = "vq:waiting"
	seqKey         = "vq:sequence"
	sessionKeyPfx  = "vq:session:"
	dataKeyPfx     = "vq:data:"
)

type Config struct {
	MaxActive         int
	Lease             time.Duration
	AverageSession    time.Duration
	PromotionInterval time.Duration
}

type Service struct {
	rdb      *redis.Client
	cfg      Config
	stop     chan struct{}
	stopOnce sync.Once
}

type Result struct {
	Status               string `json:"status"`
	SessionID            string `json:"session_id,omitempty"`
	QueueToken           string `json:"queue_token,omitempty"`
	AccessToken          string `json:"access_token,omitempty"`
	Position             int64  `json:"position,omitempty"`
	EstimatedWaitSeconds int64  `json:"estimated_wait_seconds,omitempty"`
	EstimatedWait        string `json:"estimated_wait,omitempty"`
	ExpiresIn            int64  `json:"expires_in,omitempty"`
	Message              string `json:"message,omitempty"`
}

func New(rdb *redis.Client, cfg Config) *Service {
	return &Service{
		rdb:  rdb,
		cfg:  cfg,
		stop: make(chan struct{}),
	}
}

func (s *Service) Ping(ctx context.Context) error {
	return s.rdb.Ping(ctx).Err()
}

func (s *Service) StartPromoter(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.cfg.PromotionInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := s.Promote(ctx); err != nil {
					log.Printf("promote error: %v", err)
				}
			case <-s.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (s *Service) StopPromoter() {
	s.stopOnce.Do(func() { close(s.stop) })
}

func randomToken(prefix string) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

func (s *Service) Join(ctx context.Context) (*Result, error) {
	sessionID, err := randomToken("sess")
	if err != nil {
		return nil, err
	}
	queueToken, err := randomToken("q")
	if err != nil {
		return nil, err
	}
	accessToken, err := randomToken("acc")
	if err != nil {
		return nil, err
	}

	now := time.Now().Unix()
	expiry := now + int64(s.cfg.Lease.Seconds())

	sessionKey := sessionKeyPfx + sessionID

	raw, err := scriptJoin.Run(ctx, s.rdb, []string{
		sessionKey,
		activeKey,
		seqKey,
		waitingKey,
	}, now, s.cfg.MaxActive, expiry, accessToken, dataKeyPfx, sessionID, queueToken).Result()
	if err != nil {
		return nil, err
	}

	values, ok := raw.([]interface{})
	if !ok || len(values) != 2 {
		return nil, fmt.Errorf("unexpected join response")
	}

	status := stringValue(values[0])
	token := stringValue(values[1])

	switch status {
	case "allowed":
		return s.allowedResult(ctx, token, sessionID)
	case "waiting":
		return s.waitingResult(ctx, token, sessionID)
	default:
		return nil, fmt.Errorf("unknown join status: %s", status)
	}
}

func (s *Service) Status(ctx context.Context, token string) (*Result, error) {
	data, err := s.rdb.HGetAll(ctx, dataKeyPfx+token).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return &Result{Status: "expired", Message: "queue session not found"}, nil
	}

	switch data["status"] {
	case "allowed":
		exp, _ := strconv.ParseInt(data["expires_at"], 10, 64)
		if exp <= time.Now().Unix() {
			_ = s.rdb.ZRem(ctx, activeKey, token).Err()
			_ = s.rdb.Del(ctx, dataKeyPfx+token).Err()
			return &Result{Status: "expired"}, nil
		}
		return s.allowedResult(ctx, token, data["session_id"])
	case "waiting":
		return s.waitingResult(ctx, token, data["session_id"])
	default:
		return &Result{Status: data["status"]}, nil
	}
}

func (s *Service) Heartbeat(ctx context.Context, accessToken string) (*Result, error) {
	now := time.Now().Unix()
	expiry := now + int64(s.cfg.Lease.Seconds())

	raw, err := scriptHeartbeat.Run(ctx, s.rdb, []string{activeKey, dataKeyPfx}, accessToken, now, expiry).Result()
	if err != nil {
		return nil, err
	}

	values := raw.([]interface{})
	status := stringValue(values[0])
	if status == "expired" {
		_ = s.Promote(ctx)
		return &Result{Status: "expired", Message: "access session expired"}, nil
	}

	return &Result{
		Status:      "allowed",
		AccessToken: accessToken,
		ExpiresIn:   int64(s.cfg.Lease.Seconds()),
	}, nil
}

func (s *Service) Leave(ctx context.Context, token string) (*Result, error) {
	data, err := s.rdb.HGetAll(ctx, dataKeyPfx+token).Result()
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return &Result{Status: "expired"}, nil
	}

	sessionID := data["session_id"]
	_, err = scriptLeave.Run(ctx, s.rdb, []string{activeKey, waitingKey, dataKeyPfx, sessionKeyPfx}, token, sessionID).Result()
	if err != nil {
		return nil, err
	}

	_ = s.Promote(ctx)

	return &Result{
		Status:  "left",
		Message: "queue session removed; joining again creates a new position",
	}, nil
}

func (s *Service) Promote(ctx context.Context) error {
	now := time.Now().Unix()
	_, err := scriptPromote.Run(ctx, s.rdb,
		[]string{activeKey, waitingKey, dataKeyPfx, sessionKeyPfx},
		now, s.cfg.MaxActive, int64(s.cfg.Lease.Seconds()),
	).Result()
	return err
}

func (s *Service) allowedResult(ctx context.Context, token, sessionID string) (*Result, error) {
	exp, err := s.rdb.ZScore(ctx, activeKey, token).Result()
	if err != nil {
		if err == redis.Nil {
			return &Result{Status: "expired"}, nil
		}
		return nil, err
	}

	remaining := int64(exp) - time.Now().Unix()
	if remaining <= 0 {
		return &Result{Status: "expired"}, nil
	}

	return &Result{
		Status:      "allowed",
		SessionID:   sessionID,
		AccessToken: token,
		ExpiresIn:   remaining,
	}, nil
}

func (s *Service) waitingResult(ctx context.Context, token, sessionID string) (*Result, error) {
	rank, err := s.rdb.ZRank(ctx, waitingKey, token).Result()
	if err != nil {
		if err == redis.Nil {
			return s.Status(ctx, token)
		}
		return nil, err
	}

	position := rank + 1
	activeCount, err := s.rdb.ZCard(ctx, activeKey).Result()
	if err != nil {
		return nil, err
	}

	slots := int64(s.cfg.MaxActive)
	if slots <= 0 {
		slots = 1
	}
	freePerSecond := float64(slots) / s.cfg.AverageSession.Seconds()
	waitSeconds := int64(math.Ceil(float64(position) / freePerSecond))
	if activeCount < int64(s.cfg.MaxActive) {
		waitSeconds = 0
	}

	return &Result{
		Status:               "waiting",
		SessionID:            sessionID,
		QueueToken:           token,
		Position:             position,
		EstimatedWaitSeconds: waitSeconds,
		EstimatedWait:        formatDuration(waitSeconds),
	}, nil
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "less than a minute"
	}
	minutes := (seconds + 59) / 60
	if minutes < 60 {
		return strconv.FormatInt(minutes, 10) + " minutes"
	}
	hours := minutes / 60
	remaining := minutes % 60
	if remaining == 0 {
		return strconv.FormatInt(hours, 10) + " hours"
	}
	return strconv.FormatInt(hours, 10) + " hours " +
		strconv.FormatInt(remaining, 10) + " minutes"
}

func stringValue(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return fmt.Sprint(x)
	}
}