package database

import (
	"context"
	"errors"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"go.uber.org/zap"
)

// IsSQLiteBusy reports whether err carries a SQLite BUSY result, including
// extended BUSY codes. Nil and unrelated errors return false.
func IsSQLiteBusy(err error) bool {
	var sqliteError sqlite3.Error
	return errors.As(err, &sqliteError) && sqliteError.Code == sqlite3.ErrBusy
}

// retrySQLiteBusy retries a complete idempotent database operation only for
// SQLite's BUSY family. The caller must roll back failed transactions before
// another attempt. Cancellation interrupts the wait; permanent SQL, validation
// and access errors return immediately. The total attempt count is bounded.
func (db *DB) retrySQLiteBusy(ctx context.Context, operation string, run func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := run()
		if err == nil {
			return nil
		}
		if !IsSQLiteBusy(err) {
			return err
		}
		if attempt == 3 {
			if db.logger != nil {
				db.logger.Warn("SQLite 写入重试后仍被锁阻塞", zap.String("operation", operation), zap.Int("attempts", attempt), zap.Error(err))
			}
			return err
		}
		timer := time.NewTimer(time.Duration(50<<(attempt-1)) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
