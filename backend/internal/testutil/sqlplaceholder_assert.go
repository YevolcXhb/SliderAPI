//go:build unit

package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// AssertSQLPlaceholderArgAlignment asserts that the number of positional "?"
// placeholders in a SQL query matches the number of bind arguments supplied
// alongside it. This is a test-time guard for the fragility that caused
// production Bugs A/B/C/D/E in this codebase: queries migrated from
// PostgreSQL's numbered $N placeholders (which can be reordered and reused) to
// MariaDB's positional "?" placeholders must bind exactly one argument per
// placeholder in textual order. The manual bookkeeping is error-prone; this
// helper turns the invariant into a one-line assertion in any test that can
// observe the (query, args) pair a repository emits.
//
// Comments are stripped before counting so that a "?" appearing inside a
// SQL comment (e.g. an explanatory "--" line) is not mistaken for a bind
// parameter. The MariaDB "??"" escape (a literal "?") is also handled.
//
// Usage from a recording-driver / sqlmock test:
//
//	rec := &execRecorder{Driver: driver}
//	... invoke the method under test ...
//	for _, call := range rec.calls {
//	    testutil.AssertSQLPlaceholderArgAlignment(t, call.query, call.args)
//	}
//
// This helper only checks COUNT, not positional ORDER; order must still be
// asserted structurally (e.g. that the timezone literal precedes the time
// bounds) where it matters. COUNT mismatches are the cheaper, more common
// regression and are what this helper catches deterministically.
func AssertSQLPlaceholderArgAlignment(t *testing.T, query string, args []any) {
	t.Helper()
	placeholders := CountSQLPlaceholders(query)
	require.Equalf(t, placeholders, len(args),
		"SQL placeholder/arg count mismatch:\nquery: %s\nplaceholders=%d args=%d (%v)\n"+
			"tip: each positional ? in MariaDB binds exactly one arg in order; "+
			"reused values (e.g. an amount reused across daily/weekly/monthly) "+
			"must be passed once per ? rather than referenced by name.",
		query, placeholders, len(args), args)
}

// CountSQLPlaceholders returns the number of positional "?" bind parameters in
// a SQL string after stripping comments and resolving the "??" escape. It
// mirrors the production counting intent of repository.countSQLPlaceholders
// but adds comment stripping so counts are stable on queries that carry
// explanatory "--" or "/* */" annotations (common in this codebase).
func CountSQLPlaceholders(query string) int {
	// Single literal/comment-aware scan: count "?" bind parameters while
	// skipping SQL line comments (--), block comments (/* */), and
	// single-quoted string literals (so a "?" inside a literal such as
	// 'a?b' or a JSON path is not counted). The MariaDB "??" escape (a
	// literal "?") is also handled.
	count := 0
	i := 0
	for i < len(query) {
		// Line comment: -- ... end of line.
		if i+1 < len(query) && query[i] == '-' && query[i+1] == '-' {
			i += 2
			for i < len(query) && query[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment: /* ... */ (non-nesting).
		if i+1 < len(query) && query[i] == '/' && query[i+1] == '*' {
			i += 2
			for i+1 < len(query) && !(query[i] == '*' && query[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		// Single-quoted string literal: skip its body entirely. A literal
		// ends at a single ' that is not followed by another '; a doubled ''
		// inside is an escaped quote and keeps the literal going.
		if query[i] == '\'' {
			i++
			for i < len(query) {
				if query[i] == '\'' {
					i++
					if i < len(query) && query[i] == '\'' {
						i++ // escaped quote, stay in literal
						continue
					}
					break // closing quote
				}
				i++
			}
			continue
		}
		if query[i] == '?' {
			if i+1 < len(query) && query[i+1] == '?' {
				i++ // escaped "??" is a literal '?', not a placeholder
				continue
			}
			count++
		}
		i++
	}
	return count
}
