package repository

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"ikik-api/internal/config"
	"ikik-api/internal/service"
)

// MySQLDumper implements service.DBDumper using mariadb-dump/mariadb.
type MySQLDumper struct {
	cfg *config.DatabaseConfig
}

// NewMySQLDumper creates a new MySQLDumper.
func NewMySQLDumper(cfg *config.Config) service.DBDumper {
	return &MySQLDumper{cfg: &cfg.Database}
}

// Dump executes mariadb-dump and returns a streaming reader of the output.
func (d *MySQLDumper) Dump(ctx context.Context) (io.ReadCloser, error) {
	args := []string{
		"-h", d.cfg.Host,
		"-P", fmt.Sprintf("%d", d.cfg.Port),
		"-u", d.cfg.User,
		"--single-transaction",
		"--routines",
		"--triggers",
		"--no-tablespaces",
		d.cfg.DBName,
	}

	cmd := exec.CommandContext(ctx, "mariadb-dump", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "MYSQL_PWD="+d.cfg.Password)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start mariadb-dump: %w", err)
	}

	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

// Restore executes mariadb to restore from a streaming reader.
func (d *MySQLDumper) Restore(ctx context.Context, data io.Reader) error {
	args := []string{
		"-h", d.cfg.Host,
		"-P", fmt.Sprintf("%d", d.cfg.Port),
		"-u", d.cfg.User,
		d.cfg.DBName,
	}

	cmd := exec.CommandContext(ctx, "mariadb", args...)
	if d.cfg.Password != "" {
		cmd.Env = append(cmd.Environ(), "MYSQL_PWD="+d.cfg.Password)
	}

	cmd.Stdin = data

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

// cmdReadCloser wraps a command stdout pipe and waits for the process on Close.
type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	_ = c.ReadCloser.Close()
	if err := c.cmd.Wait(); err != nil {
		return fmt.Errorf("mariadb-dump exited with error: %w", err)
	}
	return nil
}
