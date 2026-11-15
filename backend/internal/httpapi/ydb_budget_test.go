package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/said-go/vibe-check/backend/internal/vision"
)

var budgetTestNow = time.Date(2026, 11, 15, 10, 30, 0, 0, time.UTC)

func budgetRows(hour, month uint64) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"hour_start", "hour_count", "month_start", "month_count", "now"}).AddRow(
		budgetTestNow.Truncate(time.Hour), hour,
		time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), month, budgetTestNow)
}

func mockYDBBudget(t *testing.T) (*YDBBudget, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	return &YDBBudget{db: db, key: "test", hourly: 5, monthly: 100}, mock
}

func TestYDBBudgetCommitAndNoRefund(t *testing.T) {
	b, mock := mockYDBBudget(t)
	mock.ExpectBegin()
	mock.ExpectQuery(ydbBudgetRead).WithArgs("test").WillReturnRows(budgetRows(2, 9))
	mock.ExpectExec(ydbBudgetWrite).WithArgs("test", budgetTestNow.Truncate(time.Hour), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), uint64(3), uint64(10)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if wait, err := b.Take(context.Background()); wait != 0 || err != nil {
		t.Fatalf("wait=%d err=%v", wait, err)
	}
}

func TestYDBBudgetFailureNeverRetriesOrCallsAI(t *testing.T) {
	for _, stage := range []string{"begin", "read", "missing", "write", "ambiguous commit"} {
		t.Run(stage, func(t *testing.T) {
			b, mock := mockYDBBudget(t)
			failure := errors.New("private-token must not escape")
			begin := mock.ExpectBegin()
			if stage == "begin" {
				begin.WillReturnError(failure)
			} else {
				read := mock.ExpectQuery(ydbBudgetRead).WithArgs("test")
				switch stage {
				case "read":
					read.WillReturnError(failure)
				case "missing":
					read.WillReturnRows(sqlmock.NewRows([]string{"hour_start", "hour_count", "month_start", "month_count", "now"}))
				default:
					read.WillReturnRows(budgetRows(0, 0))
				}
				if stage == "write" || stage == "ambiguous commit" {
					write := mock.ExpectExec(ydbBudgetWrite)
					if stage == "write" {
						write.WillReturnError(failure)
					} else {
						write.WillReturnResult(sqlmock.NewResult(0, 1))
						mock.ExpectCommit().WillReturnError(failure)
					}
				}
				if stage != "ambiguous commit" {
					mock.ExpectRollback()
				}
			}
			client := &http.Client{Transport: budgetTransport(func(*http.Request) (*http.Response, error) {
				t.Error("budget failure reached AI provider")
				return nil, errors.New("unexpected provider call")
			})}
			provider := vision.NewYandex("key", "folder", "model", "https://example.invalid", client)
			h := NewWithLimits(slog.New(slog.NewTextHandler(io.Discard, nil)), 10<<20, provider, Limits{SharedBudget: b, PerHour: 10, PerClientMinute: 10, Concurrent: 1})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, makeImageRequest(t, []string{"true"}, [][]byte{patternedImage(t, false)}, "image/jpeg"))
			if w.Code != 503 || strings.Contains(w.Body.String(), "private-token") {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestYDBBudgetLimitsAndReadinessDoNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name        string
		hour, month uint64
		want        int
	}{
		{"hour", 5, 9, 1800}, {"month", 0, 100, int(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC).Sub(budgetTestNow).Seconds())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, mock := mockYDBBudget(t)
			mock.ExpectBegin()
			mock.ExpectQuery(ydbBudgetRead).WithArgs("test").WillReturnRows(budgetRows(tc.hour, tc.month))
			mock.ExpectRollback()
			wait, err := b.Take(context.Background())
			if wait != tc.want || err != nil {
				t.Fatalf("wait=%d err=%v", wait, err)
			}
			mock.ExpectQuery(ydbBudgetRead).WithArgs("test").WillReturnRows(budgetRows(tc.hour, tc.month))
			if err := b.Check(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestYDBBudgetWindowRolloverAndClock(t *testing.T) {
	month := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                string
		hour, month, now    time.Time
		wantHour, wantMonth uint64
		bad                 bool
	}{
		{"same", budgetTestNow.Truncate(time.Hour), month, budgetTestNow, 3, 9, false},
		{"hour", budgetTestNow.Truncate(time.Hour).Add(-time.Hour), month, budgetTestNow, 0, 9, false},
		{"month", month.Add(-time.Hour), month.AddDate(0, -1, 0), budgetTestNow, 0, 0, false},
		{"future hour", budgetTestNow.Truncate(time.Hour).Add(time.Hour), month, budgetTestNow, 0, 0, true},
		{"future month", budgetTestNow.Truncate(time.Hour), month.AddDate(0, 1, 0), budgetTestNow, 0, 0, true},
		{"bad boundary", budgetTestNow, month, budgetTestNow, 0, 0, true},
		{"empty row", time.Time{}, time.Time{}, budgetTestNow, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ydbBudgetState{hour: tc.hour, month: tc.month, now: tc.now, hourCount: 3, monthCount: 9}
			wait, err := s.prepare(5, 100)
			if (err != nil) != tc.bad {
				t.Fatalf("err=%v", err)
			}
			if !tc.bad && (wait != 0 || s.hourCount != tc.wantHour || s.monthCount != tc.wantMonth) {
				t.Fatalf("state=%+v wait=%d", s, wait)
			}
		})
	}
}

func TestYDBBudgetConfiguration(t *testing.T) {
	for _, dsn := range []string{"", "grpc://localhost:2136/?database=/local", "https://example.invalid/?database=/x", "grpcs://user:password@example.invalid/?database=/x", "grpcs://example.invalid"} {
		if _, err := NewYDBBudget(context.Background(), dsn, "key", 5, 100); err == nil {
			t.Fatal("invalid connection accepted")
		}
	}
}

// Opt-in only: the caller must provide a dedicated test database or approve a
// temporary test row in an existing provider_attempt_budget table. No schema is
// created here. The unique row is removed after testing; production keys are never used.
func TestYDBBudgetReal(t *testing.T) {
	dsn := os.Getenv("YDB_TEST_CONNECTION_STRING")
	if dsn == "" {
		t.Skip("YDB_TEST_CONNECTION_STRING is not configured")
	}
	for _, concurrency := range []int{16, 2} {
		t.Run(fmt.Sprintf("concurrency_%d", concurrency), func(t *testing.T) {
			testYDBBudgetReal(t, dsn, concurrency)
		})
	}
}

func testYDBBudgetReal(t *testing.T, dsn string, concurrency int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	key := fmt.Sprintf("integration:%d", time.Now().UnixNano())
	b, err := NewYDBBudget(ctx, dsn, key, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	t.Log("initial connection opened")
	var now time.Time
	if err = b.db.QueryRowContext(ctx, "SELECT CurrentUtcTimestamp();").Scan(&now); err != nil {
		t.Fatal("database time unavailable")
	}
	hour := now.UTC().Truncate(time.Hour)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	seed := `DECLARE $key AS Utf8; DECLARE $hour AS Timestamp; DECLARE $month AS Timestamp;
INSERT INTO provider_attempt_budget (budget_key,hour_start,hour_count,month_start,month_count)
VALUES ($key,$hour,CAST(0 AS Uint64),$month,CAST(0 AS Uint64));`
	if _, err = b.db.ExecContext(ctx, seed, sql.Named("key", key), sql.Named("hour", hour), sql.Named("month", month)); err != nil {
		t.Fatal("cannot seed test row")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := b.db.ExecContext(cleanup, `DECLARE $key AS Utf8; DELETE FROM provider_attempt_budget WHERE budget_key=$key;`, sql.Named("key", key)); err != nil {
			t.Error("test row cleanup failed")
		}
	}()
	var success atomic.Int32
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if wait, err := b.Take(ctx); err == nil && wait == 0 {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	t.Logf("concurrent successful reservations: %d", success.Load())
	if success.Load() < 1 || success.Load() > 5 {
		t.Fatalf("successful reservations=%d", success.Load())
	}
	// Each call is a separate simulated provider attempt. Conflicts and network
	// failures are allowed to fail closed; they do not prove that the window is
	// full. Keep issuing bounded distinct requests until denial is observed.
	denied := false
	for range 16 {
		wait, err := b.Take(ctx)
		if err != nil {
			continue
		}
		if wait > 0 {
			denied = true
			break
		}
		if success.Add(1) > 5 {
			t.Fatal("hourly budget admitted more than five attempts")
		}
	}
	if !denied {
		t.Fatal("hourly cap not observed before bounded probe limit")
	}
	before, err := scanYDBBudget(b.db.QueryRowContext(ctx, ydbBudgetRead, sql.Named("key", key)))
	if err != nil {
		t.Fatal("counter read failed")
	}
	if before.hourCount != 5 || before.monthCount != 5 {
		t.Fatalf("counts=%d/%d", before.hourCount, before.monthCount)
	}
	t.Log("durable counters verified: 5 hourly / 5 monthly")
	if success.Load() < 1 || success.Load() > 5 {
		t.Fatalf("invalid acknowledged reservation count: %d", success.Load())
	}
	t.Logf("actual budget denial after 5 durable reservations; acknowledged=%d, conservatively consumed without acknowledgement=%d", success.Load(), 5-success.Load())
	if err = b.Check(ctx); err != nil {
		t.Fatal(err)
	}
	t.Log("non-consuming readiness succeeded")
	after, err := scanYDBBudget(b.db.QueryRowContext(ctx, ydbBudgetRead, sql.Named("key", key)))
	if err != nil || before.hourCount != after.hourCount || before.monthCount != after.monthCount {
		t.Fatal("readiness changed budget")
	}
	other, err := NewYDBBudget(ctx, dsn, key, 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	assertYDBDenied(t, ctx, other, "new instance")
	t.Log("new client observed durable hourly denial")
	_, err = b.db.ExecContext(ctx, ydbBudgetWrite, sql.Named("key", key), sql.Named("hour", hour), sql.Named("month", month), sql.Named("hour_count", uint64(0)), sql.Named("month_count", uint64(10)))
	if err != nil {
		t.Fatal("test monthly fixture failed")
	}
	assertYDBDenied(t, ctx, other, "monthly cap")
	t.Log("monthly cap denied with hourly allowance remaining")
}

func assertYDBDenied(t *testing.T, ctx context.Context, b *YDBBudget, stage string) {
	t.Helper()
	for range 5 {
		wait, err := b.Take(ctx)
		if err != nil {
			continue
		}
		if wait <= 0 {
			t.Fatalf("%s admitted an attempt after budget exhaustion", stage)
		}
		return
	}
	t.Fatalf("%s unavailable for all bounded probes", stage)
}
