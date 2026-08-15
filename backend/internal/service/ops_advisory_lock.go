package service

import (
	"context"
	"database/sql"
	"hash/fnv"
	"time"
)

func hashAdvisoryLockID(key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}

func tryAcquireDBAdvisoryLock(ctx context.Context, db *sql.DB, lockID int64) (func(), bool) {
	if db == nil {
		return nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, false
	}

	acquired := false
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(CONCAT('ikik_api_ops_', ?), 0)", lockID).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, false
	}
	if !acquired {
		_ = conn.Close()
		return nil, false
	}

	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT RELEASE_LOCK(CONCAT('ikik_api_ops_', ?))", lockID)
		_ = conn.Close()
	}
	return release, true
}
