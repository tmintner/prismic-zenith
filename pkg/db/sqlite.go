package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type Database struct {
	Conn *sql.DB
}

func InitDB(dbPath string) (*Database, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	if err := createTables(db); err != nil {
		return nil, err
	}

	return &Database{Conn: db}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS system_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME,
			process TEXT,
			subsystem TEXT,
			category TEXT,
			level TEXT,
			message TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS system_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME,
			cpu_usage_pct REAL,
			memory_used_mb REAL,
			memory_free_mb REAL,
			disk_read_kb REAL,
			disk_write_kb REAL
		);`,
		`CREATE TABLE IF NOT EXISTS process_metrics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp DATETIME,
			pid INTEGER,
			process_name TEXT,
			cpu_pct REAL,
			memory_mb REAL
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}
	return nil
}
