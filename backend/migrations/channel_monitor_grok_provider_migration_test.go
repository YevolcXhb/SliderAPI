package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelMonitorGrokProviderMigration(t *testing.T) {
	content, err := FS.ReadFile("176_channel_monitor_grok_provider.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "channel_monitors_provider_check")
	require.Contains(t, sql, "channel_monitor_request_templates_provider_check")
	require.Contains(t, sql, "CHECK (provider IN ('openai', 'anthropic', 'gemini', 'grok'))")
	// MariaDB: pg_get_constraintdef + position() -> information_schema.check_constraints.check_clause + INSTR()
	require.Contains(t, sql, "INSTR(monitor_def, 'grok') = 0")
	require.Contains(t, sql, "INSTR(template_def, 'grok') = 0")
}
