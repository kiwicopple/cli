# Database Hot Reloading for Supabase CLI

## Overview

This document outlines a proposal to add hot reloading support for database changes in the Supabase CLI, similar to the existing hot reload functionality for edge functions.

## Background

### Current State

| Feature | Edge Functions | Database |
|---------|---------------|----------|
| File Watching | Yes (`fsnotify`) | No |
| Watch Mode | `supabase functions serve` | No equivalent |
| Auto Apply on Change | Yes (500ms debounce) | No |
| Manual Trigger Required | No | Yes |

### Current Database Workflow

1. Create a migration: `supabase migration new <name>`
2. Edit the SQL file in `supabase/migrations/`
3. Manually apply: `supabase migration up` or `supabase db reset`
4. Repeat for each change

### Why Database Hot Reload is More Complex

- **Stateful operations** - migrations depend on previous ones succeeding
- **Execution order matters** - SQL files must run in sequence
- **Risk of data loss** - auto-applying changes could be destructive
- **Rollback implications** - down migrations may be needed
- **Seed dependencies** - seeds run after migrations

---

## Proposed Solution

### New Command: `supabase db serve`

```bash
supabase db serve [flags]
```

#### Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--watch-migrations` | Watch and auto-apply new migrations | `true` |
| `--watch-seeds` | Watch seed files and re-run on change | `true` |
| `--strategy` | Watch strategy: `incremental`, `reset`, `seed-only` | `reset` |
| `--confirm-destructive` | Prompt before destructive operations | `true` |
| `--debounce` | Debounce duration in milliseconds | `1000` |

---

## Watch Strategies

### 1. `reset` (Default - Recommended for Safety)

Full database reset on any change to migrations or seeds.

**Behavior:**
- Any file change triggers `supabase db reset`
- Clean slate each time
- Safest option, no risk of inconsistent state

**Use Case:** General development, new users, complex migration chains

### 2. `incremental`

Smart detection and minimal operations.

**Behavior:**
- New migration files: Apply only the new ones
- Modified existing migration: Trigger full reset (with warning)
- Seed changes: Re-run seeds only

**Use Case:** Experienced users, large databases where reset is slow

### 3. `seed-only`

Only watch and re-run seed files.

**Behavior:**
- Ignore migration changes
- Re-run seeds on any seed file change
- Useful when iterating on test data

**Use Case:** Testing seed data, QA workflows

---

## Safety Mechanisms

### Destructive Operation Detection

```
┌─────────────────────────────────────────────────────┐
│  File Change Detected                               │
│         ↓                                           │
│  Parse SQL to detect operation type                 │
│         ↓                                           │
│  ┌─────────────────────────────────────────────┐   │
│  │ Destructive? (DROP, TRUNCATE, DELETE, ALTER)│   │
│  └─────────────────────────────────────────────┘   │
│         ↓                                           │
│    Yes: Prompt user OR skip (based on flag)        │
│    No:  Auto-apply with debounce                   │
└─────────────────────────────────────────────────────┘
```

### Destructive Keywords to Detect

- `DROP TABLE`, `DROP SCHEMA`, `DROP DATABASE`
- `TRUNCATE`
- `DELETE` (without WHERE or with `WHERE 1=1`)
- `ALTER TABLE ... DROP COLUMN`

### Modified Migration Handling

When an existing migration file is modified:

1. Detect the modification (compare file hash with last known state)
2. Warn the user that this requires a full reset
3. If `--confirm-destructive` is enabled, prompt for confirmation
4. Execute full reset if confirmed

---

## Implementation Details

### Architecture

```
cmd/db.go
    └── serveCmd
            └── internal/db/serve/serve.go
                    ├── watcher.go (reuse from functions/serve)
                    ├── changes.go (change detection)
                    └── strategy.go (strategy implementations)
```

### File Structure

```
internal/db/serve/
├── serve.go        # Main entry point and orchestration
├── watcher.go      # File watching (can import from functions/serve)
├── changes.go      # Change detection and classification
├── strategy.go     # Strategy pattern implementations
└── serve_test.go   # Tests
```

### Core Implementation

#### serve.go

```go
package serve

import (
    "context"
    "fmt"
    "os"
    "path/filepath"

    "github.com/supabase/cli/internal/utils"
    functionsWatcher "github.com/supabase/cli/internal/functions/serve"
)

type Config struct {
    Strategy           string // "reset", "incremental", "seed-only"
    ConfirmDestructive bool
    DebounceDuration   time.Duration
    WatchMigrations    bool
    WatchSeeds         bool
}

func Run(ctx context.Context, cfg Config) error {
    // 1. Initialize watcher (reuse existing infrastructure)
    watcher := functionsWatcher.NewDebounceFileWatcher()
    watcher.SetDebounceDuration(cfg.DebounceDuration)

    // 2. Set watch paths
    var watchPaths []string
    if cfg.WatchMigrations {
        watchPaths = append(watchPaths,
            filepath.Join(utils.SupabaseDirPath, "migrations"))
    }
    if cfg.WatchSeeds {
        watchPaths = append(watchPaths,
            filepath.Join(utils.SupabaseDirPath, "seed.sql"))
        // Also watch seeds directory if it exists
        seedsDir := filepath.Join(utils.SupabaseDirPath, "seeds")
        if _, err := os.Stat(seedsDir); err == nil {
            watchPaths = append(watchPaths, seedsDir)
        }
    }

    if err := watcher.SetWatchPaths(watchPaths, nil); err != nil {
        return err
    }

    // 3. Initialize change tracker
    tracker := NewChangeTracker()
    if err := tracker.Snapshot(); err != nil {
        return err
    }

    fmt.Println("Watching for database changes...")
    for _, p := range watchPaths {
        fmt.Printf("  → %s\n", p)
    }

    // 4. Main event loop
    for {
        select {
        case <-watcher.RestartCh:
            changes := tracker.DetectChanges()
            if err := handleChanges(ctx, cfg, changes); err != nil {
                fmt.Fprintf(os.Stderr, "Error: %v\n", err)
            }
            tracker.Snapshot() // Update snapshot after handling

        case err := <-watcher.ErrCh:
            fmt.Fprintf(os.Stderr, "Watcher error: %v\n", err)

        case <-ctx.Done():
            return nil
        }
    }
}
```

#### changes.go

```go
package serve

import (
    "crypto/sha256"
    "io"
    "os"
    "path/filepath"
    "strings"
)

type ChangeType int

const (
    ChangeNone ChangeType = iota
    ChangeNewMigration
    ChangeModifiedMigration
    ChangeSeedFile
)

type Changes struct {
    Type           ChangeType
    AffectedFiles  []string
    IsDestructive  bool
}

type ChangeTracker struct {
    fileHashes map[string]string
}

func NewChangeTracker() *ChangeTracker {
    return &ChangeTracker{
        fileHashes: make(map[string]string),
    }
}

func (t *ChangeTracker) Snapshot() error {
    // Walk migrations and seeds, compute hashes
    // Store in t.fileHashes
    return nil
}

func (t *ChangeTracker) DetectChanges() Changes {
    // Compare current files with snapshot
    // Classify the type of change
    return Changes{}
}

func isDestructiveSQL(content string) bool {
    destructivePatterns := []string{
        "DROP TABLE",
        "DROP SCHEMA",
        "DROP DATABASE",
        "TRUNCATE",
        "DELETE FROM",
        "DROP COLUMN",
    }
    upper := strings.ToUpper(content)
    for _, pattern := range destructivePatterns {
        if strings.Contains(upper, pattern) {
            return true
        }
    }
    return false
}
```

#### strategy.go

```go
package serve

import (
    "context"
    "fmt"

    "github.com/supabase/cli/internal/db/reset"
    "github.com/supabase/cli/internal/migration/apply"
)

func handleChanges(ctx context.Context, cfg Config, changes Changes) error {
    if changes.Type == ChangeNone {
        return nil
    }

    timestamp := time.Now().Format("15:04:05")
    fmt.Printf("[%s] Changes detected in: %v\n", timestamp, changes.AffectedFiles)

    switch cfg.Strategy {
    case "reset":
        return handleResetStrategy(ctx, cfg, changes)
    case "incremental":
        return handleIncrementalStrategy(ctx, cfg, changes)
    case "seed-only":
        return handleSeedOnlyStrategy(ctx, cfg, changes)
    default:
        return fmt.Errorf("unknown strategy: %s", cfg.Strategy)
    }
}

func handleResetStrategy(ctx context.Context, cfg Config, changes Changes) error {
    if changes.IsDestructive && cfg.ConfirmDestructive {
        if !promptConfirm("Destructive changes detected. Reset database?") {
            fmt.Println("Skipped.")
            return nil
        }
    }

    fmt.Println("Resetting database...")
    // Call existing reset logic
    return reset.Run(ctx, reset.DefaultConfig())
}

func handleIncrementalStrategy(ctx context.Context, cfg Config, changes Changes) error {
    switch changes.Type {
    case ChangeNewMigration:
        fmt.Println("Applying new migrations...")
        return apply.MigrateUp(ctx, apply.DefaultConfig())

    case ChangeModifiedMigration:
        fmt.Println("⚠️  Existing migration modified - running full reset")
        return handleResetStrategy(ctx, cfg, changes)

    case ChangeSeedFile:
        fmt.Println("Re-running seeds...")
        return apply.SeedDatabase(ctx, apply.DefaultConfig())
    }
    return nil
}

func handleSeedOnlyStrategy(ctx context.Context, cfg Config, changes Changes) error {
    if changes.Type != ChangeSeedFile {
        fmt.Println("Migration change ignored (seed-only mode)")
        return nil
    }
    fmt.Println("Re-running seeds...")
    return apply.SeedDatabase(ctx, apply.DefaultConfig())
}
```

### Command Definition

#### cmd/db.go (additions)

```go
var (
    dbServeStrategy           string
    dbServeConfirmDestructive bool
    dbServeDebounce           int
    dbServeWatchMigrations    bool
    dbServeWatchSeeds         bool

    serveCmd = &cobra.Command{
        Use:   "serve",
        Short: "Watch for database changes and auto-apply",
        Long:  `Starts a file watcher for migrations and seeds, automatically applying changes as files are modified.`,
        RunE: func(cmd *cobra.Command, args []string) error {
            cfg := serve.Config{
                Strategy:           dbServeStrategy,
                ConfirmDestructive: dbServeConfirmDestructive,
                DebounceDuration:   time.Duration(dbServeDebounce) * time.Millisecond,
                WatchMigrations:    dbServeWatchMigrations,
                WatchSeeds:         dbServeWatchSeeds,
            }
            return serve.Run(cmd.Context(), cfg)
        },
    }
)

func init() {
    dbCmd.AddCommand(serveCmd)

    serveCmd.Flags().StringVar(&dbServeStrategy, "strategy", "reset",
        "Watch strategy: reset, incremental, or seed-only")
    serveCmd.Flags().BoolVar(&dbServeConfirmDestructive, "confirm-destructive", true,
        "Prompt before applying destructive changes")
    serveCmd.Flags().IntVar(&dbServeDebounce, "debounce", 1000,
        "Debounce duration in milliseconds")
    serveCmd.Flags().BoolVar(&dbServeWatchMigrations, "watch-migrations", true,
        "Watch migration files")
    serveCmd.Flags().BoolVar(&dbServeWatchSeeds, "watch-seeds", true,
        "Watch seed files")
}
```

---

## Configuration

### config.toml Support

```toml
[db.serve]
enabled = true
strategy = "reset"              # "reset", "incremental", "seed-only"
confirm_destructive = true
debounce_ms = 1000
watch_migrations = true
watch_seeds = true
watch_paths = [                 # Additional paths to watch
    "supabase/seeds/*.sql"
]
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SUPABASE_DB_SERVE_STRATEGY` | Override strategy | `reset` |
| `SUPABASE_DB_WATCH_LIMIT` | Max files to watch | `1000` |

---

## User Experience

### Example Session

```
$ supabase db serve

Watching for database changes...
  → supabase/migrations/
  → supabase/seed.sql

[12:01:15] Changes detected in: [migrations/20240115120115_add_users.sql]
[12:01:15] Applying new migration: 20240115120115_add_users.sql
[12:01:16] Migration applied successfully ✓

[12:02:30] Changes detected in: [seed.sql]
[12:02:30] Re-running seeds...
[12:02:31] Seeds applied successfully ✓

[12:03:45] Changes detected in: [migrations/20240110100000_create_tables.sql]
[12:03:45] ⚠️  Existing migration modified - this requires a full reset
           All data will be lost. Continue? [y/N]: y
[12:03:47] Resetting database...
[12:03:52] Database reset complete ✓

^C
Shutting down watcher...
```

### Error Handling

```
[12:04:00] Changes detected in: [migrations/20240115_bad_syntax.sql]
[12:04:00] Applying migration...
[12:04:01] ✗ Migration failed:
           ERROR: syntax error at or near "CRAETE" at line 1

           Fix the error and save to retry.
```

---

## Files to Create/Modify

| File | Action | Description |
|------|--------|-------------|
| `cmd/db.go` | Modify | Add `serveCmd` subcommand |
| `internal/db/serve/serve.go` | Create | Main serve orchestration |
| `internal/db/serve/watcher.go` | Create | File watching (or import from functions) |
| `internal/db/serve/changes.go` | Create | Change detection and classification |
| `internal/db/serve/strategy.go` | Create | Strategy implementations |
| `internal/db/serve/serve_test.go` | Create | Unit tests |
| `pkg/config/config.go` | Modify | Add `[db.serve]` config section |

### Potential Refactoring

The existing watcher in `internal/functions/serve/watcher.go` could be extracted to a shared package:

```
internal/utils/watcher/
├── watcher.go      # Generic file watcher
└── watcher_test.go
```

This would allow both `functions serve` and `db serve` to use the same infrastructure.

---

## Testing Plan

### Unit Tests

1. **Change Detection**
   - Detect new migration files
   - Detect modified migration files
   - Detect seed file changes
   - Handle multiple simultaneous changes

2. **Destructive SQL Detection**
   - Identify DROP statements
   - Identify TRUNCATE statements
   - Identify DELETE without WHERE
   - Handle case variations

3. **Strategy Logic**
   - Reset strategy triggers reset on any change
   - Incremental strategy applies only new migrations
   - Seed-only strategy ignores migration changes

### Integration Tests

1. **File Watcher**
   - Watcher starts and stops cleanly
   - Debouncing works correctly
   - Multiple rapid changes coalesce

2. **End-to-End**
   - Create migration, verify auto-applied
   - Modify seed, verify re-seeded
   - Modify existing migration, verify reset triggered

---

## Rollout Plan

### Phase 1: Basic Implementation
- Implement `reset` strategy only
- Watch migrations and seeds
- Basic change detection

### Phase 2: Advanced Strategies
- Add `incremental` strategy
- Add `seed-only` strategy
- Destructive operation detection

### Phase 3: Polish
- Configuration file support
- Better error messages
- Progress indicators
- Documentation

---

## Open Questions

1. **Should we support down migrations?**
   - If a migration file is deleted, should we run its down migration?
   - Risk: Could cause data loss
   - Recommendation: No, require manual intervention

2. **How to handle migration ordering issues?**
   - What if a new migration has a timestamp before existing applied migrations?
   - Recommendation: Warn and suggest reset

3. **Should seeds be transactional?**
   - Wrap seed execution in a transaction for atomicity?
   - Recommendation: Yes, with option to disable

4. **Watch limit behavior?**
   - What happens when watch limit is exceeded?
   - Recommendation: Warn and fall back to polling or suggest increasing limit

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Accidental data loss | High | Default to `reset` strategy with confirmation prompts |
| Performance with large DBs | Medium | Debouncing, incremental strategy option |
| Race conditions | Medium | Proper locking, sequential application |
| Watcher crashes | Low | Graceful error handling, auto-restart option |

---

## Success Metrics

- Developer iteration speed improvement (time from code change to seeing result)
- Reduction in manual `db reset` commands during development
- User adoption rate of `db serve` command
- Error rate during hot reload operations

---

## References

- Existing edge functions watcher: `internal/functions/serve/watcher.go`
- Database reset implementation: `internal/db/reset/reset.go`
- Migration apply logic: `internal/migration/apply/apply.go`
- Configuration system: `pkg/config/config.go`
