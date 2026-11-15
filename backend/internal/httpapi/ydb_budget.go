package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"net/url"
	"os"
	"regexp"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	yc "github.com/ydb-platform/ydb-go-yc"
)

// YDBBudget uses one durable, pre-provisioned row for both UTC calendar windows.
// Missing rows fail closed: deleting a row must not silently renew the budget.
// All instances must use the same key and limits. No reservation is refunded.
type YDBBudget struct {
	db              *sql.DB
	driver          *ydb.Driver
	key             string
	hourly, monthly uint64
}

const ydbBudgetRead = `DECLARE $key AS Utf8;
SELECT hour_start, hour_count, month_start, month_count, CurrentUtcTimestamp() AS now
FROM provider_attempt_budget WHERE budget_key = $key;`

const ydbBudgetWrite = `DECLARE $key AS Utf8;
DECLARE $hour AS Timestamp;
DECLARE $month AS Timestamp;
DECLARE $hour_count AS Uint64;
DECLARE $month_count AS Uint64;
UPDATE provider_attempt_budget
SET hour_start = $hour, hour_count = $hour_count,
    month_start = $month, month_count = $month_count
WHERE budget_key = $key;`

var errYDBBudget = errors.New("shared budget unavailable")
var ydbBudgetKey = regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,128}$`)

// NewYDBBudget uses the container service account via metadata by default.
// An explicit YDB_ACCESS_TOKEN_CREDENTIALS environment value supports controlled
// local integration checks. Credentials and SDK error details are never returned.
// It never creates tables or initializes counters.
func NewYDBBudget(ctx context.Context, connectionString, key string, hourly, monthly int) (*YDBBudget, error) {
	u, err := url.Parse(connectionString)
	if err != nil || u.Scheme != "grpcs" || u.Host == "" || u.User != nil || u.Query().Get("database") == "" || !ydbBudgetKey.MatchString(key) || hourly < 1 || monthly < 1 {
		return nil, errors.New("invalid YDB budget configuration")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	credential := yc.WithMetadataCredentials()
	if token := os.Getenv("YDB_ACCESS_TOKEN_CREDENTIALS"); token != "" {
		credential = ydb.WithAccessTokenCredentials(token)
	}
	driver, err := ydb.Open(ctx, connectionString, credential)
	if err != nil {
		return nil, errors.New("shared budget unavailable: connection initialization failed")
	}
	connector, err := ydb.Connector(driver, ydb.WithQueryService(true))
	if err != nil {
		_ = driver.Close(ctx)
		return nil, errors.New("shared budget unavailable: SQL connector initialization failed")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	// OpenDB is lazy. Complete the initial session handshake during bounded
	// startup instead of spending a first analysis's five-second budget on it.
	if db.PingContext(ctx) != nil {
		_ = db.Close()
		_ = driver.Close(ctx)
		return nil, errors.New("shared budget unavailable: initial session probe failed")
	}
	return &YDBBudget{db: db, driver: driver, key: key, hourly: uint64(hourly), monthly: uint64(monthly)}, nil
}

type ydbBudgetState struct {
	hour, month, now      time.Time
	hourCount, monthCount uint64
}

type budgetScanner interface{ Scan(...any) error }

func scanYDBBudget(row budgetScanner) (ydbBudgetState, error) {
	var s ydbBudgetState
	err := row.Scan(&s.hour, &s.hourCount, &s.month, &s.monthCount, &s.now)
	return s, err
}

// prepare uses database time, never instance wall clocks. A backwards time jump
// or malformed stored window fails closed rather than resetting a counter.
func (s *ydbBudgetState) prepare(hourly, monthly uint64) (int, error) {
	s.now, s.hour, s.month = s.now.UTC(), s.hour.UTC(), s.month.UTC()
	hour := s.now.Truncate(time.Hour)
	month := time.Date(s.now.Year(), s.now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if s.now.IsZero() || s.hour.IsZero() || s.month.IsZero() || s.hour.After(hour) || s.month.After(month) || !s.hour.Equal(s.hour.Truncate(time.Hour)) || s.month.Day() != 1 || s.month.Hour() != 0 || s.month.Minute() != 0 || s.month.Second() != 0 || s.month.Nanosecond() != 0 {
		return 0, errYDBBudget
	}
	if s.hour.Before(hour) {
		s.hour, s.hourCount = hour, 0
	}
	if s.month.Before(month) {
		s.month, s.monthCount = month, 0
	}
	var wait time.Duration
	if s.hourCount >= hourly {
		wait = hour.Add(time.Hour).Sub(s.now)
	}
	if s.monthCount >= monthly {
		if monthlyWait := month.AddDate(0, 1, 0).Sub(s.now); monthlyWait > wait {
			wait = monthlyWait
		}
	}
	return int(math.Ceil(wait.Seconds())), nil
}

func (b *YDBBudget) Take(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// Explicit transactions are essential: never use retry.DoTx or an SDK Do
	// wrapper here. A lost commit reply is an unavailable budget, not permission
	// to call AI or to replay the reservation. Conflicts also fail closed.
	tx, err := b.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return 0, errYDBBudget
	}
	defer tx.Rollback()
	s, err := scanYDBBudget(tx.QueryRowContext(ctx, ydbBudgetRead, sql.Named("key", b.key)))
	if err != nil {
		return 0, errYDBBudget
	}
	wait, err := s.prepare(b.hourly, b.monthly)
	if err != nil {
		return 0, errYDBBudget
	}
	if wait > 0 {
		return wait, nil
	}
	_, err = tx.ExecContext(ctx, ydbBudgetWrite, sql.Named("key", b.key),
		sql.Named("hour", s.hour), sql.Named("month", s.month),
		sql.Named("hour_count", s.hourCount+1), sql.Named("month_count", s.monthCount+1))
	if err != nil {
		return 0, errYDBBudget
	}
	if tx.Commit() != nil {
		return 0, errYDBBudget
	}
	return 0, nil
}

func (b *YDBBudget) Check(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	s, err := scanYDBBudget(b.db.QueryRowContext(ctx, ydbBudgetRead, sql.Named("key", b.key)))
	if err != nil {
		return errYDBBudget
	}
	_, err = s.prepare(b.hourly, b.monthly)
	return err
}

func (b *YDBBudget) Close() error {
	err := b.db.Close()
	if b.driver != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if closeErr := b.driver.Close(ctx); closeErr != nil {
			return errYDBBudget
		}
	}
	if err != nil {
		return errYDBBudget
	}
	return nil
}
