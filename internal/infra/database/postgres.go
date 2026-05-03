package database

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func NewPostgresDB(connStr string) (*sqlx.DB, error) {
	db, err := sqlx.Connect("postgres", connStr)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	return db, nil
}

func MigrateDir(db *sqlx.DB, dir string) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	// Ensure order (e.g. 001_init.sql, 002_users.sql)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".sql" {
			continue
		}

		path := filepath.Join(dir, file.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		fmt.Println("Running migration:", file.Name())

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed at %s: %w", file.Name(), err)
		}
	}

	return nil
}