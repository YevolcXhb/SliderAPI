package repository

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Int64CSV encodes []int64 as a comma-separated string for FIND_IN_SET.
type Int64CSV []int64

func (a Int64CSV) Value() (driver.Value, error) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, ","), nil
}

// IntCSV encodes []int as a comma-separated string for FIND_IN_SET.
type IntCSV []int

func (a IntCSV) Value() (driver.Value, error) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ","), nil
}

// Int32CSV encodes []int32 as a comma-separated string for FIND_IN_SET.
type Int32CSV []int32

func (a Int32CSV) Value() (driver.Value, error) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.FormatInt(int64(v), 10)
	}
	return strings.Join(parts, ","), nil
}

// StringCSV encodes []string as a comma-separated string for FIND_IN_SET.
type StringCSV []string

func (a StringCSV) Value() (driver.Value, error) {
	return strings.Join(a, ","), nil
}

// Float64CSV encodes []float64 as a comma-separated string for FIND_IN_SET.
type Float64CSV []float64

func (a Float64CSV) Value() (driver.Value, error) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strings.Join(parts, ","), nil
}

// JSONInt64Slice reads/writes []int64 as a JSON array (JSON columns).
type JSONInt64Slice []int64

func (a JSONInt64Slice) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *JSONInt64Slice) Scan(src any) error {
	if src == nil {
		*a = nil
		return nil
	}
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, a)
	case string:
		return json.Unmarshal([]byte(v), a)
	default:
		return fmt.Errorf("cannot scan %T into JSONInt64Slice", src)
	}
}

// int64Float64PairsValue marshals parallel slices into [[id, value], ...] for JSON_TABLE.
func int64Float64PairsValue(ids []int64, values []float64) (driver.Value, error) {
	if len(ids) != len(values) {
		return nil, fmt.Errorf("int64/float64 pair length mismatch")
	}
	pairs := make([][2]any, len(ids))
	for i := range ids {
		pairs[i] = [2]any{ids[i], values[i]}
	}
	return json.Marshal(pairs)
}

// int64IntPairsValue marshals parallel slices into [[id, value], ...] for JSON_TABLE.
func int64IntPairsValue(ids []int64, values []int) (driver.Value, error) {
	if len(ids) != len(values) {
		return nil, fmt.Errorf("int64/int pair length mismatch")
	}
	pairs := make([][2]any, len(ids))
	for i := range ids {
		pairs[i] = [2]any{ids[i], values[i]}
	}
	return json.Marshal(pairs)
}

// int64StringPairsValue marshals parallel slices into [[id, value], ...] for JSON_TABLE.
func int64StringPairsValue(ids []int64, values []string) (driver.Value, error) {
	if len(ids) != len(values) {
		return nil, fmt.Errorf("int64/string pair length mismatch")
	}
	pairs := make([][2]any, len(ids))
	for i := range ids {
		pairs[i] = [2]any{ids[i], values[i]}
	}
	return json.Marshal(pairs)
}

// mustInt64Float64PairsValue panics on length mismatch (call sites guarantee parity).
func mustInt64Float64PairsValue(ids []int64, values []float64) driver.Value {
	v, err := int64Float64PairsValue(ids, values)
	if err != nil {
		panic(err)
	}
	return v
}

// mustInt64Int32PairsValue panics on length mismatch.
func mustInt64Int32PairsValue(ids []int64, values []int32) driver.Value {
	if len(ids) != len(values) {
		panic("int64/int32 pair length mismatch")
	}
	pairs := make([][2]any, len(ids))
	for i := range ids {
		pairs[i] = [2]any{ids[i], values[i]}
	}
	b, err := json.Marshal(pairs)
	if err != nil {
		panic(err)
	}
	return driver.Value(b)
}

// mustInt64StringPairsValue panics on length mismatch.
func mustInt64StringPairsValue(ids []int64, values []string) driver.Value {
	v, err := int64StringPairsValue(ids, values)
	if err != nil {
		panic(err)
	}
	return v
}
