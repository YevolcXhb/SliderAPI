package repository

import (
	"database/sql"
	"testing"
	"time"

	"ikik-api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestBuildDBPoolSettings(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			MaxOpenConns:           50,
			MaxIdleConns:           10,
			ConnMaxLifetimeMinutes: 30,
			ConnMaxIdleTimeMinutes: 5,
		},
	}

	settings := buildDBPoolSettings(cfg)
	require.Equal(t, 50, settings.MaxOpenConns)
	require.Equal(t, 10, settings.MaxIdleConns)
	require.Equal(t, 30*time.Minute, settings.ConnMaxLifetime)
	require.Equal(t, 5*time.Minute, settings.ConnMaxIdleTime)
}

func TestApplyDBPoolSettings(t *testing.T) {
	cfg := &config.Config{
		Database: config.DatabaseConfig{
			MaxOpenConns:           40,
			MaxIdleConns:           8,
			ConnMaxLifetimeMinutes: 15,
			ConnMaxIdleTimeMinutes: 3,
		},
	}

	db, err := sql.Open("mysql", "root:root@tcp(127.0.0.1:3306)/test")
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})

	applyDBPoolSettings(db, cfg)
	stats := db.Stats()
	require.Equal(t, 40, stats.MaxOpenConnections)
}
