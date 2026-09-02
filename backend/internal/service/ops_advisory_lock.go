package service

import (
	"context"
	"database/sql"
	"hash/fnv"
	"strconv"
	"time"
)

// hashAdvisoryLockID converts a stable lock key into a 64-bit integer that
// the original PostgreSQL advisory-lock API consumed. MySQL/MariaDB
// GET_LOCK needs a string name; we keep the int64-based API for caller
// stability and stringify here.
func hashAdvisoryLockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

// tryAcquireDBAdvisoryLock takes a single-instance named lock using
// MySQL/MariaDB GET_LOCK. Returns a release func and acquired flag.
//
// The implementation uses a single 64-character string derived from
// lockID so it stays within MariaDB's GET_LOCK name length limit (64 in
// 10.4+, 10.11 is 64).
func tryAcquireDBAdvisoryLock(ctx context.Context, db *sql.DB, lockID int64) (func(), bool) {
	release, acquired, _ := tryAcquireDBAdvisoryLockWithError(ctx, db, lockID)
	return release, acquired
}

func tryAcquireDBAdvisoryLockWithError(ctx context.Context, db *sql.DB, lockID int64) (func(), bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	// 5 second acquire timeout, like the prior PG implementation.
	lockName := "sub2api_" + strconv.FormatInt(lockID, 10)
	if len(lockName) > 64 {
		lockName = lockName[:64]
	}
	acquired := false
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, 5)", lockName).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false, err
	}
	if !acquired {
		_ = conn.Close()
		return nil, false, nil
	}
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// RELEASE_LOCK returns 1 on success, 0 if the lock was not held.
		// We intentionally ignore the result so release is best-effort.
		_, _ = conn.ExecContext(unlockCtx, "SELECT RELEASE_LOCK(?)", lockName)
		_ = conn.Close()
	}
	return release, true, nil
}
