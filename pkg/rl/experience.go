package rl

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// Experience represents a single interaction with the LLM and its outcome.
type Experience struct {
	ID              int64
	Timestamp       time.Time
	Source          string // "query" or "recommend"
	Prompt          string
	GeneratedQuery  string
	ExecutionResult string // Details of success or failure
	UserFeedback    int    // 0 = none, 1 = good, -1 = bad
}

// DB handles the connection to the experience replay SQLite database.
type DB struct {
	sqlDB *sql.DB
}

// InitDB creates or opens the SQLite database for storing RL experiences.
func InitDB(dbPath string) (*DB, error) {
	err := os.MkdirAll(filepath.Dir(dbPath), 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create db directory: %v", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %v", err)
	}

	createTableSQL := `
	CREATE TABLE IF NOT EXISTS experiences (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		source TEXT NOT NULL,
		prompt TEXT NOT NULL,
		generated_query TEXT,
		execution_result TEXT,
		user_feedback INTEGER DEFAULT 0
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to create table: %v", err)
	}

	return &DB{sqlDB: db}, nil
}

// LogExperience records an LLM interaction and its immediate execution result.
// It returns the ID of the inserted record, which can be used later for user feedback.
func (db *DB) LogExperience(source, prompt, generatedQuery, executionResult string) (int64, error) {
	insertSQL := `
	INSERT INTO experiences (source, prompt, generated_query, execution_result)
	VALUES (?, ?, ?, ?)`

	stmt, err := db.sqlDB.Prepare(insertSQL)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	res, err := stmt.Exec(source, prompt, generatedQuery, executionResult)
	if err != nil {
		return 0, err
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	log.Printf("RL Experience Logged [ID: %d] Source: %s", id, source)
	return id, nil
}

// UpdateFeedback updates the user_feedback field for a specific experience ID.
func (db *DB) UpdateFeedback(id int64, feedback int) error {
	updateSQL := `UPDATE experiences SET user_feedback = ? WHERE id = ?`

	stmt, err := db.sqlDB.Prepare(updateSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(feedback, id)
	if err != nil {
		return err
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("experience ID %d not found", id)
	}

	log.Printf("RL Experience Feedback Updated [ID: %d] Feedback: %d", id, feedback)
	return nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	if db.sqlDB != nil {
		return db.sqlDB.Close()
	}
	return nil
}
