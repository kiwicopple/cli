# Plan: Implement `supabase db watch` Command

## Overview

Add a new `supabase db watch` command that watches the `supabase/schemas/` directory for changes and automatically generates migrations by running `db diff` when schema files are modified.

## Architecture

The watch command will reuse existing components:
- **File Watcher**: Reuse `DebounceFileWatcher` from `internal/functions/serve/watcher.go`
- **Diff Logic**: Reuse `diff.DiffDatabase` and `diff.SaveDiff` from `internal/db/diff/`
- **Schema Loading**: Reuse `loadDeclaredSchemas` pattern from `internal/db/diff/diff.go`

## Implementation Steps

### Step 1: Create Watch Package

Create new file: `internal/db/watch/watch.go`

```go
package watch

import (
    "context"
    "fmt"
    "os"

    "github.com/jackc/pgconn"
    "github.com/jackc/pgx/v4"
    "github.com/spf13/afero"
    "github.com/supabase/cli/internal/db/diff"
    "github.com/supabase/cli/internal/utils"
)

type WatchConfig struct {
    Schema   []string           // Schemas to diff
    Differ   diff.DiffFunc      // Diff function (migra, pg-schema, etc.)
    DbConfig pgconn.Config      // Database connection config
    AutoSave bool               // Auto-save migrations with generated names
}

func Run(ctx context.Context, config WatchConfig, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
    // 1. Check database is running
    // 2. Get watch paths from config or default to SchemasDir
    // 3. Initialize file watcher
    // 4. Main watch loop:
    //    - On file change: run diff
    //    - On diff output: optionally save migration
    //    - Handle graceful shutdown via context
}
```

### Step 2: Move Watcher to Shared Package

The `DebounceFileWatcher` is currently in `internal/functions/serve/`. We have two options:

**Option A (Recommended)**: Import directly from `internal/functions/serve/watcher.go`
- Pros: No code duplication
- Cons: Cross-package dependency

**Option B**: Create a shared watcher package at `internal/utils/watcher/`
- Pros: Clean separation
- Cons: More files to maintain

For now, use Option A and import from `internal/functions/serve`.

### Step 3: Add Command Definition

Update `cmd/db.go` to add the watch command:

```go
var (
    autoSave bool

    dbWatchCmd = &cobra.Command{
        Use:   "watch",
        Short: "Watch schema files and generate migrations on change",
        Long:  "Watches supabase/schemas/ directory for changes and automatically generates migrations using db diff.",
        RunE: func(cmd *cobra.Command, args []string) error {
            differ := diff.DiffSchemaMigra
            if usePgSchema {
                differ = diff.DiffPgSchema
            }
            config := watch.WatchConfig{
                Schema:   schema,
                Differ:   differ,
                DbConfig: flags.DbConfig,
                AutoSave: autoSave,
            }
            return watch.Run(cmd.Context(), config, afero.NewOsFs())
        },
    }
)

func init() {
    // ... existing init code ...

    // Build watch command
    watchFlags := dbWatchCmd.Flags()
    watchFlags.StringSliceVarP(&schema, "schema", "s", []string{}, "Comma separated list of schema to include.")
    watchFlags.BoolVar(&autoSave, "auto-save", false, "Automatically save migrations with generated names.")
    watchFlags.BoolVar(&usePgSchema, "use-pg-schema", false, "Use pg-schema-diff to generate schema diff.")
    dbCmd.AddCommand(dbWatchCmd)
}
```

### Step 4: Implement Watch Logic

Full implementation for `internal/db/watch/watch.go`:

```go
package watch

import (
    "context"
    "fmt"
    "os"
    "path/filepath"
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
)

type WatchConfig struct {
    Schema   []string
    Differ   diff.DiffFunc
    DbConfig pgconn.Config
    AutoSave bool
}

func Run(ctx context.Context, config WatchConfig, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
    // 1. Sanity check - ensure database is running
    if err := utils.AssertSupabaseDbIsRunning(); err != nil {
        return err
    }

    // 2. Determine watch paths
    watchPaths, err := getWatchPaths(fsys)
    if err != nil {
        return err
    }
    if len(watchPaths) == 0 {
        return errors.New("No schema files to watch. Create files in supabase/schemas/ or configure db.migrations.schema_paths in config.toml")
    }

    // 3. Initialize watcher
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

    // 4. Main watch loop
    for {
        select {
        case <-ctx.Done():
            fmt.Fprintln(os.Stderr, "\nStopping watch...")
            return nil
        case <-watcher.RestartCh:
            if err := runDiff(ctx, config, fsys, options...); err != nil {
                // Log error but continue watching
                fmt.Fprintf(os.Stderr, "Error: %v\n", err)
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
        return nil, err
    } else if exists {
        return []string{utils.SchemasDir}, nil
    }

    return nil, nil
}

func runDiff(ctx context.Context, config WatchConfig, fsys afero.Fs, options ...func(*pgx.ConnConfig)) error {
    fmt.Fprintln(os.Stderr, strings.Repeat("-", 40))
    fmt.Fprintln(os.Stderr, "Schema change detected, generating diff...")

    out, err := diff.DiffDatabase(ctx, config.Schema, config.DbConfig, os.Stderr, fsys, config.Differ, options...)
    if err != nil {
        return err
    }

    if len(out) < 2 {
        fmt.Fprintln(os.Stderr, "No schema changes found")
        return nil
    }

    // Generate migration file name if auto-save enabled
    file := ""
    if config.AutoSave {
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

    fmt.Fprintln(os.Stderr)
    return nil
}

func generateMigrationName() string {
    return fmt.Sprintf("schema_change_%s", time.Now().Format("150405"))
}

// Reuse drop detection from diff package (may need to export or duplicate)
func findDropStatements(out string) []string {
    // Implementation similar to diff.findDropStatements
    return nil
}
```

### Step 5: Export Required Functions

In `internal/db/diff/diff.go`, export `loadDeclaredSchemas` by renaming to `LoadDeclaredSchemas` (if needed for watch package).

Alternatively, duplicate the logic in the watch package to avoid changing the diff package API.

## Flags Summary

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--schema` | `-s` | `[]` | Comma-separated list of schemas to diff |
| `--auto-save` | | `false` | Auto-save migrations with timestamp names |
| `--use-pg-schema` | | `false` | Use pg-schema-diff instead of migra |

## User Experience

```bash
# Basic usage - watch and print diffs to stdout
supabase db watch

# Auto-save migrations
supabase db watch --auto-save

# Watch specific schemas
supabase db watch --schema public,auth

# Output example:
# Watching schema files on branch main...
# Press Ctrl+C to stop.
#
# ----------------------------------------
# File change detected: supabase/schemas/employees.sql (WRITE)
# Schema change detected, generating diff...
# Creating shadow database...
# Diffing schemas...
#
# alter table "public"."employees" add column "age" smallint not null;
#
# ----------------------------------------
# File change detected: supabase/schemas/employees.sql (WRITE)
# ...
```

## Files to Create/Modify

| File | Action |
|------|--------|
| `internal/db/watch/watch.go` | Create |
| `cmd/db.go` | Modify (add watch command) |

## Testing Considerations

1. Unit tests for `getWatchPaths()` function
2. Integration test for the watch loop (with mock watcher)
3. Manual testing:
   - Create/modify/delete schema files
   - Verify debouncing works correctly
   - Test Ctrl+C graceful shutdown
   - Test with `--auto-save` flag

## Future Enhancements

1. **Interactive mode**: Prompt user before saving each migration
2. **Migration naming**: Allow custom naming pattern via flag
3. **Lint on save**: Run `db lint` after generating migration
4. **Apply migrations**: Option to auto-apply migrations after generation
