package dblock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// AcquireNamedLock acquires a MySQL/MariaDB named lock on a dedicated
// connection and returns a release function that both releases the lock and
// closes the connection. timeoutSeconds <= 0 means try once without waiting.
func AcquireNamedLock(ctx context.Context, db *sql.DB, name string, timeoutSeconds int) (func(), error) {
	if db == nil {
		return nil, errors.New("nil sql db")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}

	var acquired int
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, ?)", name, timeoutSeconds).Scan(&acquired); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if acquired != 1 {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to acquire mysql named lock %q", name)
	}

	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = conn.ExecContext(unlockCtx, "SELECT RELEASE_LOCK(?)", name)
		_ = conn.Close()
	}
	return release, nil
}
