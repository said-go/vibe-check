// init-ydb-budget is an explicit provisioning tool, never run by the API server.
// It creates the table if absent and INSERTs a missing counter row. Existing
// counters are never reset, including when two provisioning commands race.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"time"

	ydb "github.com/ydb-platform/ydb-go-sdk/v3"
	"github.com/ydb-platform/ydb-go-sdk/v3/query"
	yc "github.com/ydb-platform/ydb-go-yc"
)

const createBudgetTable = `CREATE TABLE IF NOT EXISTS provider_attempt_budget (
budget_key Utf8 NOT NULL,
hour_start Timestamp,
hour_count Uint64,
month_start Timestamp,
month_count Uint64,
PRIMARY KEY (budget_key)
);`

const findBudgetRow = `DECLARE $key AS Utf8;
SELECT budget_key FROM provider_attempt_budget WHERE budget_key=$key;`

const insertBudgetRow = `DECLARE $key AS Utf8;
DECLARE $hour AS Timestamp;
DECLARE $month AS Timestamp;
INSERT INTO provider_attempt_budget (budget_key,hour_start,hour_count,month_start,month_count)
VALUES ($key,$hour,CAST(0 AS Uint64),$month,CAST(0 AS Uint64));`

func main() {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: init-ydb-budget\nEnvironment: YDB_CONNECTION_STRING (grpcs DSN), YDB_BUDGET_KEY.\nOptional: YDB_ACCESS_TOKEN_CREDENTIALS; otherwise service-account metadata authentication.\nCreates table and INSERTs missing row only; never resets existing counters.")
	}
	flag.Parse()
	if flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	created, err := run(ctx, os.Getenv("YDB_CONNECTION_STRING"), os.Getenv("YDB_BUDGET_KEY"), os.Getenv("YDB_ACCESS_TOKEN_CREDENTIALS"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if created {
		fmt.Println("Budget table ready; new counter row initialized.")
	} else {
		fmt.Println("Budget counter row already exists; unchanged.")
	}
}

func run(ctx context.Context, dsn, key, token string) (bool, error) {
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "grpcs" || u.Host == "" || u.User != nil || u.Query().Get("database") == "" || !regexp.MustCompile(`^[a-zA-Z0-9:_-]{1,128}$`).MatchString(key) {
		return false, errors.New("invalid YDB provisioning configuration")
	}
	credential := yc.WithMetadataCredentials()
	if token != "" {
		credential = ydb.WithAccessTokenCredentials(token)
	}
	driver, err := ydb.Open(ctx, dsn, credential)
	if err != nil {
		return false, errors.New("YDB connection unavailable")
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = driver.Close(closeCtx)
	}()
	connector, err := ydb.Connector(driver, ydb.WithQueryService(true))
	if err != nil {
		return false, errors.New("YDB SQL connection unavailable")
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	return provision(ctx, db, key)
}

func provision(ctx context.Context, db *sql.DB, key string) (bool, error) {
	// DDL must run without a data transaction; all row operations below are
	// protected by a serializable transaction and INSERT's primary-key check.
	if _, err := db.ExecContext(ydb.WithTxControl(ctx, query.NoTx()), createBudgetTable); err != nil {
		return false, errors.New("budget table creation failed")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return false, errors.New("budget initialization transaction unavailable")
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, findBudgetRow, sql.Named("key", key)).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, errors.New("budget row lookup failed")
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, "SELECT CurrentUtcTimestamp();").Scan(&now); err != nil || now.IsZero() {
		return false, errors.New("database time unavailable")
	}
	now = now.UTC()
	hour := now.Truncate(time.Hour)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	if _, err = tx.ExecContext(ctx, insertBudgetRow, sql.Named("key", key), sql.Named("hour", hour), sql.Named("month", month)); err != nil {
		return false, errors.New("budget row insert failed; existing counters were not overwritten")
	}
	if err = tx.Commit(); err != nil {
		return false, errors.New("budget initialization commit unconfirmed; inspect row before retrying")
	}
	return true, nil
}
