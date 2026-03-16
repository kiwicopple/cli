package watch

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/jackc/pgx/v4"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/db/diff"
	"github.com/supabase/cli/internal/functions/serve"
	"github.com/supabase/cli/internal/gen/keys"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/parser"
)

type DiffFunc = diff.DiffFunc

func Run(ctx context.Context, schema []string, config pgconn.Config, differ DiffFunc, autoSave bool, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	// Sanity check - ensure database is running
	if err := utils.AssertSupabaseDbIsRunning(); err != nil {
		return err
	}

	// Determine watch paths
	watchPaths, err := getWatchPaths(fsys)
	if err != nil {
		return err
	}
	if len(watchPaths) == 0 {
		return errors.New("No schema files to watch. Create files in supabase/schemas/ or configure db.migrations.schema_paths in config.toml")
	}

	// Initialize watcher
	watcher, err := serve.NewDebounceFileWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.SetWatchPaths(watchPaths, fsys); err != nil {
		return err
	}

	// Start watcher goroutine
	go watcher.Start()

	branch := keys.GetGitBranch(fsys)
	fmt.Fprintf(os.Stderr, "Watching schema files on branch %s...\n", utils.Aqua(branch))
	fmt.Fprintf(os.Stderr, "Press Ctrl+C to stop.\n\n")

	// Main watch loop
	for {
		select {
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nStopping watch...")
			return nil
		case <-watcher.RestartCh:
			if err := runDiff(ctx, schema, config, differ, autoSave, fsys, options...); err != nil {
				// Log error but continue watching
				fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
			}
		case err := <-watcher.ErrCh:
			return errors.Errorf("watcher error: %w", err)
		}
	}
}

func getWatchPaths(fsys afero.Fs) ([]string, error) {
	// Check config for custom schema paths
	if schemas := utils.Config.Db.Migrations.SchemaPaths; len(schemas) > 0 {
		return schemas.Files(afero.NewIOFS(fsys))
	}

	// Fall back to default schemas directory
	if exists, err := afero.DirExists(fsys, utils.SchemasDir); err != nil {
		return nil, errors.Errorf("failed to check schemas directory: %w", err)
	} else if exists {
		return []string{utils.SchemasDir}, nil
	}

	return nil, nil
}

func runDiff(ctx context.Context, schema []string, config pgconn.Config, differ DiffFunc, autoSave bool, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
	fmt.Fprintln(os.Stderr, strings.Repeat("-", 40))
	fmt.Fprintln(os.Stderr, "Schema change detected, generating diff...")

	out, err := diff.DiffDatabase(ctx, schema, config, os.Stderr, fsys, differ, options...)
	if err != nil {
		return err
	}

	// Generate migration file name if auto-save enabled
	file := ""
	if autoSave {
		file = generateMigrationName()
	}

	if err := diff.SaveDiff(out, file, fsys); err != nil {
		return err
	}

	// Show drop statement warnings
	drops := findDropStatements(out)
	if len(drops) > 0 {
		fmt.Fprintln(os.Stderr, utils.Yellow("Found drop statements:"))
		fmt.Fprintln(os.Stderr, utils.Yellow(strings.Join(drops, "\n")))
	}

	branch := keys.GetGitBranch(fsys)
	fmt.Fprintln(os.Stderr, "Finished "+utils.Aqua("supabase db diff")+" on branch "+utils.Aqua(branch)+".\n")
	return nil
}

func generateMigrationName() string {
	return fmt.Sprintf("schema_change_%s", time.Now().Format("150405"))
}

// https://github.com/djrobstep/migra/blob/master/migra/statements.py#L6
var dropStatementPattern = regexp.MustCompile(`(?i)drop\s+`)

func findDropStatements(out string) []string {
	lines, err := parser.SplitAndTrim(strings.NewReader(out))
	if err != nil {
		return nil
	}
	var drops []string
	for _, line := range lines {
		if dropStatementPattern.MatchString(line) {
			drops = append(drops, line)
		}
	}
	return drops
}
