package database

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	_ "github.com/go-sql-driver/mysql"
	sqldblogger "github.com/simukti/sqldb-logger"

	"mccwk.com/lk/models"
)

// const dateFmt = "2006-01-02 15:04:05"

type Database struct {
	Filename string
	Conn     *sql.DB
	Queries  *models.Queries
}

func New(connStr string) *Database {
	conn, err := sql.Open("mysql", connStr)
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}

	// Wrap connection with SQL logger
	// logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// loggerAdapter := NewSlogAdapter(logger)
	// conn = sqldblogger.OpenDriver(connStr, conn.Driver(), loggerAdapter, sqldblogger.WithSQLQueryFieldname("sql"))

	db := Database{}
	db.Conn = conn
	db.Queries = models.New(db.Conn)

	return &db
}

func (db *Database) Close() error {
	return db.Conn.Close()
}

// SlogAdapter implements sqldblogger.Logger interface for slog
type SlogAdapter struct {
	logger *slog.Logger
}

func NewSlogAdapter(logger *slog.Logger) *SlogAdapter {
	return &SlogAdapter{logger: logger}
}

func (s *SlogAdapter) Log(ctx context.Context, level sqldblogger.Level, msg string, data map[string]interface{}) {
	var slogLevel slog.Level
	switch level {
	case sqldblogger.LevelError:
		slogLevel = slog.LevelError
	case sqldblogger.LevelInfo:
		slogLevel = slog.LevelInfo
	case sqldblogger.LevelDebug:
		slogLevel = slog.LevelDebug
	default:
		slogLevel = slog.LevelInfo
	}

	attrs := make([]slog.Attr, 0, len(data))
	for k, v := range data {
		attrs = append(attrs, slog.Any(k, v))
	}

	s.logger.LogAttrs(ctx, slogLevel, msg, attrs...)
}
