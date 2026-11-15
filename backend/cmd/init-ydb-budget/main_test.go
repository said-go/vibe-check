package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestProvisionNeverResetsExistingCounters(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(createBudgetTable).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectBegin()
	mock.ExpectQuery(findBudgetRow).WithArgs("test").WillReturnRows(sqlmock.NewRows([]string{"budget_key"}).AddRow("test"))
	mock.ExpectRollback()
	created, err := provision(context.Background(), db, "test")
	if created || err != nil {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionInsertAndAmbiguousCommit(t *testing.T) {
	for _, stage := range []string{"success", "insert conflict", "ambiguous commit"} {
		t.Run(stage, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			failure := errors.New("secret-token")
			mock.ExpectExec(createBudgetTable).WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectBegin()
			mock.ExpectQuery(findBudgetRow).WithArgs("test").WillReturnRows(sqlmock.NewRows([]string{"budget_key"}))
			now := time.Date(2026, 11, 15, 10, 30, 1, 0, time.UTC)
			mock.ExpectQuery("SELECT CurrentUtcTimestamp();").WillReturnRows(sqlmock.NewRows([]string{"now"}).AddRow(now))
			insert := mock.ExpectExec(insertBudgetRow).WithArgs("test", now.Truncate(time.Hour), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
			if stage == "insert conflict" {
				insert.WillReturnError(failure)
				mock.ExpectRollback()
			} else {
				insert.WillReturnResult(sqlmock.NewResult(0, 1))
				commit := mock.ExpectCommit()
				if stage == "ambiguous commit" {
					commit.WillReturnError(failure)
				}
			}
			created, err := provision(context.Background(), db, "test")
			if (stage == "success") != created || (err == nil) != (stage == "success") {
				t.Fatalf("created=%v err=%v", created, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-token") {
				t.Fatal("private error leaked")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProvisionRejectsUnsafeConfiguration(t *testing.T) {
	for _, tc := range []struct{ dsn, key string }{{"", "key"}, {"grpcs://user:pass@host/?database=/db", "key"}, {"grpc://host/?database=/db", "key"}, {"grpcs://host/?database=/db", "bad;key"}} {
		if _, err := run(context.Background(), tc.dsn, tc.key, "secret-token"); err == nil || strings.Contains(err.Error(), "secret-token") {
			t.Fatal("bad configuration accepted or leaked")
		}
	}
}
