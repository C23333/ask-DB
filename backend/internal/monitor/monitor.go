package monitor

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/smtp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/yourusername/db_asst/config"
)

// Event represents a single metric row stored in DB.
type Event struct {
	ID        string                 `json:"id"`
	EventType string                 `json:"event_type"`
	Duration  int64                  `json:"duration_ms"`
	Success   bool                   `json:"success"`
	Extra     map[string]interface{} `json:"extra,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// Stats aggregates metrics for one event type.
type Stats struct {
	EventType    string    `json:"event_type"`
	Count        int       `json:"count"`
	SuccessCount int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	AvgDuration  float64   `json:"avg_duration_ms"`
	P95Duration  float64   `json:"p95_duration_ms"`
	MaxDuration  int64     `json:"max_duration_ms"`
	LastEventAt  time.Time `json:"last_event_at"`
	LastError    string    `json:"last_error,omitempty"`
}

// Summary describes global metrics for a time window.
type Summary struct {
	Total           int     `json:"total"`
	Success         int     `json:"success"`
	Fail            int     `json:"fail"`
	SuccessRate     float64 `json:"success_rate"`
	AvgDuration     float64 `json:"avg_duration_ms"`
	WindowHours     float64 `json:"window_hours"`
	AlertingEnabled bool    `json:"alerting_enabled"`
}

// TrendPoint represents aggregated metrics per time bucket.
type TrendPoint struct {
	Bucket       time.Time `json:"bucket"`
	TotalCount   int       `json:"total_count"`
	SuccessCount int       `json:"success_count"`
	FailCount    int       `json:"fail_count"`
	AvgDuration  float64   `json:"avg_duration_ms"`
}

// Dashboard bundles stats, trends and recent events for UI use.
type Dashboard struct {
	Summary Summary      `json:"summary"`
	Stats   []Stats      `json:"stats"`
	Trend   []TrendPoint `json:"trend"`
	Recent  []Event      `json:"recent"`
}

// UserUsage aggregates metrics per user.
type UserUsage struct {
	UserID        string    `json:"user_id"`
	TotalCalls    int       `json:"total_calls"`
	SuccessCount  int       `json:"success_count"`
	FailCount     int       `json:"fail_count"`
	AvgDuration   float64   `json:"avg_duration_ms"`
	TotalDuration int64     `json:"total_duration_ms"`
	LastEventAt   time.Time `json:"last_event_at"`
}

// Monitor handles metric persistence and alerting.
type Monitor struct {
	db        *sql.DB
	driver    string
	cfg       *config.Config
	logger    *zap.Logger
	filters   map[string]struct{}
	lastAlert map[string]time.Time
	minDur    time.Duration
	cooldown  time.Duration
	mu        sync.Mutex
}

// New creates a monitor service and ensures the table exists.
func New(db *sql.DB, driver string, cfg *config.Config, logger *zap.Logger) (*Monitor, error) {
	if db == nil {
		return nil, errors.New("monitor requires db handle")
	}
	m := &Monitor{
		db:        db,
		driver:    driver,
		cfg:       cfg,
		logger:    logger,
		filters:   make(map[string]struct{}),
		lastAlert: make(map[string]time.Time),
		minDur:    time.Duration(cfg.EmailAlertMinDurationMs) * time.Millisecond,
		cooldown:  time.Duration(cfg.EmailAlertCooldownSec) * time.Second,
	}
	for _, evt := range cfg.EmailAlertEventTypes {
		evt = strings.TrimSpace(evt)
		if evt == "" {
			continue
		}
		m.filters[evt] = struct{}{}
	}
	if m.cooldown <= 0 {
		m.cooldown = 5 * time.Minute
	}
	if err := m.ensureTable(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Monitor) ensureTable() error {
	switch m.driver {
	case "mysql":
		const ddl = `CREATE TABLE IF NOT EXISTS system_metrics (
            id CHAR(36) PRIMARY KEY,
            event_type VARCHAR(64) NOT NULL,
            duration_ms BIGINT,
            success TINYINT(1) NOT NULL,
            extra JSON,
            created_at DATETIME NOT NULL,
            KEY idx_metrics_event (event_type, created_at)
        ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`
		_, err := m.db.Exec(ddl)
		return err
	case "oracle":
		const check = `SELECT COUNT(*) FROM USER_TABLES WHERE TABLE_NAME = 'SYSTEM_METRICS'`
		var count int
		if err := m.db.QueryRow(check).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		const ddl = `CREATE TABLE SYSTEM_METRICS (
            ID VARCHAR2(36) PRIMARY KEY,
            EVENT_TYPE VARCHAR2(64) NOT NULL,
            DURATION_MS NUMBER,
            SUCCESS NUMBER(1) NOT NULL,
            EXTRA CLOB,
            CREATED_AT TIMESTAMP NOT NULL
        )`
		if _, err := m.db.Exec(ddl); err != nil {
			return err
		}
		_, err := m.db.Exec(`CREATE INDEX IDX_METRICS_EVENT ON SYSTEM_METRICS (EVENT_TYPE, CREATED_AT)`)
		return err
	default:
		return fmt.Errorf("unsupported driver %s", m.driver)
	}
}

// Record persists an event and triggers alert when needed.
func (m *Monitor) Record(eventType string, duration time.Duration, success bool, extra map[string]interface{}) {
	if m == nil || m.db == nil {
		return
	}
	id := uuid.New().String()
	now := time.Now()
	var payload []byte
	if extra != nil {
		payload, _ = json.Marshal(extra)
	}
	switch m.driver {
	case "oracle":
		_, err := m.db.Exec(`INSERT INTO SYSTEM_METRICS (ID, EVENT_TYPE, DURATION_MS, SUCCESS, EXTRA, CREATED_AT) VALUES (:1, :2, :3, :4, :5, :6)`,
			id, eventType, duration.Milliseconds(), boolToInt(success), string(payload), now)
		if err != nil {
			m.logger.Warn("record metric failed", zap.Error(err))
		}
	default:
		_, err := m.db.Exec(`INSERT INTO system_metrics (id, event_type, duration_ms, success, extra, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, eventType, duration.Milliseconds(), boolToInt(success), string(payload), now)
		if err != nil {
			m.logger.Warn("record metric failed", zap.Error(err))
		}
	}
	if !success {
		m.sendAlert(eventType, duration, extra)
	}
}

// QueryStats aggregates metrics per event type for a time window.
func (m *Monitor) QueryStats(from, to time.Time) ([]Stats, error) {
	events, err := m.queryEvents(from, to)
	if err != nil {
		return nil, err
	}
	stats, _ := aggregateStats(events, from, to, m.alertConfigured())
	return stats, nil
}

// QueryDashboard returns stats + trend + recent events.
func (m *Monitor) QueryDashboard(from, to time.Time, bucketMinutes int) (*Dashboard, error) {
	events, err := m.queryEvents(from, to)
	if err != nil {
		return nil, err
	}
	stats, summary := aggregateStats(events, from, to, m.alertConfigured())
	trend := buildTrend(events, bucketMinutes)
	recent := pickRecent(events, 20)
	return &Dashboard{
		Summary: summary,
		Stats:   stats,
		Trend:   trend,
		Recent:  recent,
	}, nil
}

// QueryUserUsage aggregates metrics grouped by user_id over a time range.
func (m *Monitor) QueryUserUsage(from, to time.Time) ([]UserUsage, error) {
	events, err := m.queryEvents(from, to)
	if err != nil {
		return nil, err
	}
	usageMap := make(map[string]*UserUsage)
	for _, evt := range events {
		userID, _ := evt.Extra["user_id"].(string)
		if strings.TrimSpace(userID) == "" {
			continue
		}
		current := usageMap[userID]
		if current == nil {
			current = &UserUsage{UserID: userID}
			usageMap[userID] = current
		}
		current.TotalCalls++
		if evt.Success {
			current.SuccessCount++
		} else {
			current.FailCount++
		}
		current.TotalDuration += evt.Duration
		if evt.CreatedAt.After(current.LastEventAt) {
			current.LastEventAt = evt.CreatedAt
		}
	}
	result := make([]UserUsage, 0, len(usageMap))
	for _, usage := range usageMap {
		if usage.TotalCalls > 0 {
			usage.AvgDuration = float64(usage.TotalDuration) / float64(usage.TotalCalls)
		}
		result = append(result, *usage)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalCalls == result[j].TotalCalls {
			return result[i].UserID < result[j].UserID
		}
		return result[i].TotalCalls > result[j].TotalCalls
	})
	return result, nil
}

func (m *Monitor) queryEvents(from, to time.Time) ([]Event, error) {
	query := `SELECT id, event_type, duration_ms, success, extra, created_at
        FROM system_metrics WHERE created_at BETWEEN ? AND ? ORDER BY created_at ASC`
	args := []interface{}{from, to}
	if m.driver == "oracle" {
		query = `SELECT id, event_type, duration_ms, success, extra, created_at
        FROM SYSTEM_METRICS WHERE created_at BETWEEN :1 AND :2 ORDER BY created_at ASC`
	}
	rows, err := m.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		ev := Event{}
		var duration sql.NullInt64
		var success sql.NullInt64
		var extraRaw sql.NullString
		if err := rows.Scan(&ev.ID, &ev.EventType, &duration, &success, &extraRaw, &ev.CreatedAt); err != nil {
			return nil, err
		}
		if duration.Valid {
			ev.Duration = duration.Int64
		}
		ev.Success = success.Valid && success.Int64 == 1
		if extraRaw.Valid && strings.TrimSpace(extraRaw.String) != "" {
			if err := json.Unmarshal([]byte(extraRaw.String), &ev.Extra); err != nil {
				m.logger.Debug("failed to unmarshal monitor extra", zap.Error(err))
			}
		}
		events = append(events, ev)
	}
	return events, nil
}

func (m *Monitor) sendAlert(eventType string, duration time.Duration, extra map[string]interface{}) {
	if !m.alertConfigured() || !m.isAlertAllowed(eventType, duration) {
		return
	}
	extraText := ""
	if len(extra) > 0 {
		if payload, err := json.MarshalIndent(extra, "", "  "); err == nil {
			extraText = string(payload)
		} else {
			extraText = fmt.Sprintf("%v", extra)
		}
	}
	if extraText == "" {
		extraText = "(none)"
	}
	subject := fmt.Sprintf("[DB Assistant] %s failed", eventType)
	body := fmt.Sprintf("Event: %s\nDuration: %s\nTime: %s\nExtra:\n%s",
		eventType,
		duration,
		time.Now().Format(time.RFC3339),
		extraText)
	recipients := make([]string, 0)
	for _, addr := range strings.Split(m.cfg.EmailAlertTo, ",") {
		if trimmed := strings.TrimSpace(addr); trimmed != "" {
			recipients = append(recipients, trimmed)
		}
	}
	if len(recipients) == 0 {
		return
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		m.cfg.EmailSMTPUser,
		strings.Join(recipients, ","),
		subject,
		body)
	auth := smtp.PlainAuth("", m.cfg.EmailSMTPUser, m.cfg.EmailSMTPPassword, m.cfg.EmailSMTPHost)
	addr := fmt.Sprintf("%s:%d", m.cfg.EmailSMTPHost, m.cfg.EmailSMTPPort)
	if err := smtp.SendMail(addr, auth, m.cfg.EmailSMTPUser, recipients, []byte(msg)); err != nil {
		m.logger.Warn("failed to send alert email", zap.Error(err))
	}
}

func (m *Monitor) alertConfigured() bool {
	return m.cfg != nil &&
		m.cfg.EmailSMTPHost != "" &&
		m.cfg.EmailSMTPUser != "" &&
		m.cfg.EmailSMTPPassword != "" &&
		m.cfg.EmailAlertTo != ""
}

func (m *Monitor) isAlertAllowed(eventType string, duration time.Duration) bool {
	if len(m.filters) > 0 {
		if _, ok := m.filters[eventType]; !ok {
			return false
		}
	}
	if m.minDur > 0 && duration < m.minDur {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	last := m.lastAlert[eventType]
	if !last.IsZero() && time.Since(last) < m.cooldown {
		return false
	}
	m.lastAlert[eventType] = time.Now()
	return true
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type statsAccumulator struct {
	stat      Stats
	durations []int64
	lastEvent time.Time
	maxDur    int64
}

func aggregateStats(events []Event, from, to time.Time, alerting bool) ([]Stats, Summary) {
	summary := Summary{
		WindowHours:     to.Sub(from).Hours(),
		AlertingEnabled: alerting,
	}
	statsMap := make(map[string]*statsAccumulator)
	for _, evt := range events {
		summary.Total++
		if evt.Success {
			summary.Success++
		} else {
			summary.Fail++
		}
		summary.AvgDuration += float64(evt.Duration)

		acc := statsMap[evt.EventType]
		if acc == nil {
			acc = &statsAccumulator{
				stat: Stats{EventType: evt.EventType},
			}
			statsMap[evt.EventType] = acc
		}
		acc.stat.Count++
		if evt.Success {
			acc.stat.SuccessCount++
		} else {
			acc.stat.FailCount++
			if acc.stat.LastError == "" {
				if errVal, ok := evt.Extra["error"]; ok {
					acc.stat.LastError = fmt.Sprint(errVal)
				}
			}
		}
		acc.stat.AvgDuration += float64(evt.Duration)
		acc.durations = append(acc.durations, evt.Duration)
		if evt.Duration > acc.maxDur {
			acc.maxDur = evt.Duration
		}
		if evt.CreatedAt.After(acc.lastEvent) {
			acc.lastEvent = evt.CreatedAt
		}
	}

	var stats []Stats
	for _, acc := range statsMap {
		if acc.stat.Count > 0 {
			acc.stat.AvgDuration /= float64(acc.stat.Count)
		}
		acc.stat.P95Duration = percentile(acc.durations, 0.95)
		acc.stat.MaxDuration = acc.maxDur
		acc.stat.LastEventAt = acc.lastEvent
		stats = append(stats, acc.stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		return stats[i].EventType < stats[j].EventType
	})

	if summary.Total > 0 {
		summary.AvgDuration /= float64(summary.Total)
		summary.SuccessRate = float64(summary.Success) / float64(summary.Total)
	}
	return stats, summary
}

func percentile(values []int64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]int64(nil), values...)
	sort.Slice(copied, func(i, j int) bool {
		return copied[i] < copied[j]
	})
	if p <= 0 {
		return float64(copied[0])
	}
	if p >= 1 {
		return float64(copied[len(copied)-1])
	}
	idx := p * float64(len(copied)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return float64(copied[lower])
	}
	weight := idx - float64(lower)
	return float64(copied[lower]) + weight*(float64(copied[upper])-float64(copied[lower]))
}

func buildTrend(events []Event, bucketMinutes int) []TrendPoint {
	if bucketMinutes <= 0 {
		bucketMinutes = 15
	}
	window := time.Duration(bucketMinutes) * time.Minute
	buckets := make(map[time.Time]*TrendPoint)
	for _, evt := range events {
		bucket := evt.CreatedAt.Truncate(window)
		point := buckets[bucket]
		if point == nil {
			point = &TrendPoint{Bucket: bucket}
			buckets[bucket] = point
		}
		point.TotalCount++
		if evt.Success {
			point.SuccessCount++
		} else {
			point.FailCount++
		}
		point.AvgDuration += float64(evt.Duration)
	}
	var trend []TrendPoint
	for _, point := range buckets {
		if point.TotalCount > 0 {
			point.AvgDuration /= float64(point.TotalCount)
		}
		trend = append(trend, *point)
	}
	sort.Slice(trend, func(i, j int) bool {
		return trend[i].Bucket.Before(trend[j].Bucket)
	})
	return trend
}

func pickRecent(events []Event, limit int) []Event {
	if limit <= 0 || len(events) == 0 {
		return []Event{}
	}
	if len(events) > limit {
		events = events[len(events)-limit:]
	}
	result := make([]Event, len(events))
	for i := range events {
		result[i] = events[len(events)-1-i]
	}
	return result
}
