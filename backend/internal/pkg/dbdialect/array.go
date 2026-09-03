package dbdialect

import (
	"fmt"
	"strings"
)

// ArrayJoin is a MariaDB alternative to pq.Array for use with FIND_IN_SET.
// It converts a slice of any type to a comma-separated string.
func ArrayJoin[T any](arr []T) string {
	if len(arr) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, v := range arr {
		if i > 0 {
			sb.WriteString(",")
		}
		_ = sb.WriteString(fmt.Sprint(v))
	}
	return sb.String()
}
