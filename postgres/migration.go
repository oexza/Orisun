package postgres

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed scripts/common/*.sql
var sqlScripts embed.FS

//go:embed scripts/admin/*.sql
var adminSqlScripts embed.FS

func RunDbScripts(db *sql.DB, schema string, isAdminSchema bool, ctx context.Context) error {
	// First run common migrations
	if err := runMigrationsInFolder(db, sqlScripts, schema, ctx); err != nil {
		return fmt.Errorf("failed to run common migrations: %w", err)
	}

	// If this is admin schema, run admin migrations
	if isAdminSchema {
		if err := runMigrationsInFolder(db, adminSqlScripts, schema, ctx); err != nil {
			return fmt.Errorf("failed to run admin migrations: %w", err)
		}
	}
	return nil
}

func runMigrationsInFolder(db *sql.DB, scripts embed.FS, schema string, ctx context.Context) error {
	// Schema creation code remains the same
	if schema != "public" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema)); err != nil {
			return fmt.Errorf("failed to create schema %s: %w", schema, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit schema creation: %w", err)
		}
	}

	var combinedScript strings.Builder
	combinedScript.WriteString(fmt.Sprintf("SET search_path TO %s;", schema))

	// Recursively find and process all SQL files
	if err := processEmbeddedSQLFiles(scripts, &combinedScript, ""); err != nil {
		return fmt.Errorf("failed to process SQL files: %w", err)
	}

	// Transaction execution remains the same
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, combinedScript.String()); err != nil {
		return fmt.Errorf("failed to execute migrations: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Helper function to recursively process SQL files
func processEmbeddedSQLFiles(fs embed.FS, builder *strings.Builder, dir string) error {
	// Determine the base directory based on which embedded filesystem we're using
	var baseDir string
	if fs == sqlScripts {
		baseDir = "scripts/common"
	} else if fs == adminSqlScripts {
		baseDir = "scripts/admin"
	} else {
		return fmt.Errorf("unknown script type")
	}

	// If dir is empty, use the base directory
	if dir == "" {
		dir = baseDir
	}

	entries, err := fs.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	// Sort entries alphabetically by name to ensure deterministic execution order
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		path := dir + "/" + entry.Name()

		if entry.IsDir() {
			// Recursively process subdirectories
			if err := processEmbeddedSQLFiles(fs, builder, path); err != nil {
				return err
			}
		} else if strings.HasSuffix(entry.Name(), ".sql") {
			// Process SQL file
			content, err := fs.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read file %s: %w", path, err)
			}

			builder.WriteString(fmt.Sprintf("\n-- Executing %s\n", path))
			builder.WriteString(string(content))
			builder.WriteString("\n")
		}
	}

	return nil
}
