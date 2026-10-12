package database

import (
	"database/sql"
	"time"
)

// The API speaks epoch milliseconds (like Date.now() in the demo); the database
// stores TIMESTAMPTZ. These helpers convert at the repository boundary.

// Ms converts a time to epoch milliseconds.
func Ms(t time.Time) int64 {
	return t.UnixMilli()
}

// NullMs converts a nullable time to *epoch ms.
func NullMs(t sql.NullTime) *int64 {
	if !t.Valid {
		return nil
	}
	v := t.Time.UnixMilli()
	return &v
}

// FromMs converts epoch ms to a time for query arguments.
func FromMs(ms int64) time.Time {
	return time.UnixMilli(ms)
}

// FromMsPtr converts *epoch ms to a nullable query argument.
func FromMsPtr(ms *int64) any {
	if ms == nil {
		return nil
	}
	return time.UnixMilli(*ms)
}

// NullString converts a nullable string column.
func NullString(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	v := s.String
	return &v
}

// NullFloat converts a nullable float column.
func NullFloat(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}
	v := f.Float64
	return &v
}

// NullInt converts a nullable integer column.
func NullInt(i sql.NullInt64) *int64 {
	if !i.Valid {
		return nil
	}
	v := i.Int64
	return &v
}

// NowMs is the current time in epoch ms.
func NowMs() int64 {
	return time.Now().UnixMilli()
}
