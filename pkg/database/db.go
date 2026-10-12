package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/pkg/logger"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

type Database struct {
	DB *sql.DB
}

// NewDatabase creates a new database connection using database/sql.
// DATABASE_URL (shared with Prisma) is used when set, otherwise the DB_* fields.
func NewDatabase(cfg *config.DatabaseConfig) (*Database, error) {
	db, err := sql.Open("postgres", cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Connection pool settings. The database is remote and shared, so keep the pool modest.
	db.SetMaxIdleConns(5)
	db.SetMaxOpenConns(20)
	db.SetConnMaxLifetime(time.Hour)

	logger.Info("Database connected successfully", zap.String("db", cfg.Label()))

	return &Database{DB: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.DB.Close()
}

// TX is the subset of database/sql satisfied by both *sql.DB and *sql.Tx.
// Repositories use this interface for context propagation and transaction scoping;
// QueryBuilder.ExecuteContext and the Raw*Context helpers accept both.
type TX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
