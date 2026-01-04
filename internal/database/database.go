package database

import (
	"database/sql"
	"embed"
	"log/slog"
	"os"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"

	"mccwk.com/lk/internal/models"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

type Database struct {
	Filename string
	Conn     *sql.DB
	Queries  *models.Queries
}

func New(dbPath string) *Database {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	// Enable foreign keys
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		slog.Error("failed to enable foreign keys", "error", err)
		os.Exit(1)
	}

	db := &Database{
		Filename: dbPath,
		Conn:     conn,
		Queries:  models.New(conn),
	}

	// Run migrations
	if err := db.RunMigrations(); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	return db
}

func (db *Database) Close() error {
	return db.Conn.Close()
}

func (db *Database) RunMigrations() error {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}

	if err := goose.Up(db.Conn, "migrations"); err != nil {
		return err
	}

	return nil
}
