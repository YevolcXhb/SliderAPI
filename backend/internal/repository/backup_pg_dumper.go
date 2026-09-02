package repository

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// MariaDBDumper implements database dump/restore with the MariaDB client
// utilities shipped by the application image.
type MariaDBDumper struct {
	cfg *config.DatabaseConfig
}

// NewMariaDBDumper creates the production MariaDB dump/restore adapter.
func NewMariaDBDumper(cfg *config.Config) service.DBDumper {
	return &MariaDBDumper{cfg: &cfg.Database}
}

// NewPgDumper is retained as a source-compatible constructor alias. It no
// longer invokes PostgreSQL tools; the returned implementation always uses
// mariadb-dump/mariadb.
func NewPgDumper(cfg *config.Config) service.DBDumper { return NewMariaDBDumper(cfg) }

// Dump executes mariadb-dump and returns a streaming reader of SQL output.
func (d *MariaDBDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	args := []string{
		"--host", d.cfg.Host,
		"--port", strconv.Itoa(d.cfg.Port),
		"--user", d.cfg.User,
		"--databases", d.cfg.DBName,
		"--single-transaction",
		"--quick",
		"--routines",
		"--events",
		"--triggers",
		"--add-drop-table",
	}
	cmd := exec.CommandContext(ctx, "mariadb-dump", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "MYSQL_PWD="+d.cfg.Password)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create mariadb-dump stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mariadb-dump: %w", err)
	}
	return &mariaDBCmdReadCloser{ReadCloser: stdout, cmd: cmd, command: "mariadb-dump"}, nil
}

// Restore executes the MariaDB client against a streaming SQL input.
func (d *MariaDBDumper) Restore(ctx context.Context, data io.Reader) error {
	args := []string{
		"--host", d.cfg.Host,
		"--port", strconv.Itoa(d.cfg.Port),
		"--user", d.cfg.User,
		"--database", d.cfg.DBName,
		"--force",
	}
	cmd := exec.CommandContext(ctx, "mariadb", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "MYSQL_PWD="+d.cfg.Password)
	}
	cmd.Stdin = data
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("mariadb restore: %w: %s", err, string(output))
	}
	return nil
}

type mariaDBCmdReadCloser struct {
	io.ReadCloser
	cmd     *exec.Cmd
	command string
}

func (c *mariaDBCmdReadCloser) Close() error {
	_ = c.ReadCloser.Close()
	if err := c.cmd.Wait(); err != nil {
		return fmt.Errorf("%s exited with error: %w", c.command, err)
	}
	return nil
}

// cmdReadCloser is retained for package-local tests/source compatibility.
type cmdReadCloser = mariaDBCmdReadCloser
