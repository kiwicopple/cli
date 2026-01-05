# Plan: Structured Database Dump for Declarative Schemas

## Overview

Implement a new mode for the `db dump` command that outputs the database structure into an organized directory hierarchy, enabling declarative schema management. This will allow users to version-control their database schema as individual SQL files that can be composed using the existing `schema_paths` glob pattern system.

## Current State

### Existing Functionality
- `db dump` outputs schema/data/roles to a single file or stdout (`internal/db/dump/dump.go`)
- `db diff` already supports declarative schemas via `schema_paths` config (`internal/db/diff/diff.go:51-73`)
- Glob pattern system in `pkg/config/config.go` provides ordered file matching with deduplication
- SQL parser in `pkg/parser/` can split statements while handling complex PostgreSQL syntax
- Default schemas directory: `supabase/schemas/` (`internal/utils/misc.go:80`)

### Config Pattern Support
```toml
[db.migrations]
schema_paths = ["./schemas/*.sql"]  # Already supports glob patterns
```

## Proposed Directory Structure

Mirror PostgreSQL's logical organization:

```
supabase/
├── schemas/
│   ├── 00_extensions.sql       # Extensions (cluster-wide)
│   ├── 01_roles.sql            # Custom roles and grants
│   ├── 02_schemas.sql          # CREATE SCHEMA statements
│   │
│   ├── public/                 # One directory per schema
│   │   ├── 00_types.sql        # Composite types, enums, domains
│   │   ├── 01_sequences.sql    # Sequences (before tables that reference them)
│   │   ├── tables/
│   │   │   ├── users.sql       # Each table in its own file
│   │   │   └── posts.sql
│   │   ├── views/
│   │   │   └── user_posts.sql
│   │   ├── functions/
│   │   │   └── get_user.sql
│   │   ├── triggers/
│   │   │   └── update_timestamp.sql
│   │   └── policies/
│   │       └── users_rls.sql
│   │
│   └── private/                # Additional schemas
│       └── ... (same structure)
│
├── migrations/                 # Existing migrations
└── config.toml
```

### Rationale for Structure

1. **Numeric prefixes (00_, 01_, 02_)** - Ensures correct load order within glob patterns
2. **Extensions first** - Required before schemas that depend on extension types
3. **Roles before schemas** - Roles may own schemas
4. **Types before tables** - Tables may reference custom types
5. **Sequences before tables** - Tables may have default values referencing sequences
6. **Tables before views/functions** - Views query tables, functions may reference tables
7. **Triggers after tables/functions** - Triggers reference both
8. **Policies last** - RLS policies reference tables

### Recommended Config Pattern

```toml
[db.migrations]
schema_paths = [
  # Cluster-level objects
  "./schemas/00_extensions.sql",
  "./schemas/01_roles.sql",
  "./schemas/02_schemas.sql",

  # Schema-level objects (per schema, in order)
  "./schemas/*/00_types.sql",
  "./schemas/*/01_sequences.sql",
  "./schemas/*/tables/*.sql",
  "./schemas/*/views/*.sql",
  "./schemas/*/functions/*.sql",
  "./schemas/*/triggers/*.sql",
  "./schemas/*/policies/*.sql",
]
```

## Implementation Plan

### Phase 1: SQL Statement Classifier

**File:** `pkg/migration/classify.go`

Create a statement classifier that parses pg_dump output and categorizes each statement:

```go
type StatementType string

const (
    TypeExtension  StatementType = "extension"
    TypeRole       StatementType = "role"
    TypeSchema     StatementType = "schema"
    TypeType       StatementType = "type"      // ENUM, COMPOSITE, DOMAIN
    TypeSequence   StatementType = "sequence"
    TypeTable      StatementType = "table"
    TypeView       StatementType = "view"
    TypeFunction   StatementType = "function"
    TypeTrigger    StatementType = "trigger"
    TypePolicy     StatementType = "policy"
    TypeIndex      StatementType = "index"
    TypeConstraint StatementType = "constraint"
    TypeGrant      StatementType = "grant"
    TypeComment    StatementType = "comment"
    TypeOther      StatementType = "other"
)

type ClassifiedStatement struct {
    Type       StatementType
    Schema     string    // e.g., "public"
    ObjectName string    // e.g., "users"
    Statement  string    // Full SQL statement
}

func ClassifyStatement(sql string) ClassifiedStatement
```

**Classification patterns:**
- `CREATE EXTENSION` → extension
- `CREATE ROLE` / `ALTER ROLE` / `GRANT ... TO` → role
- `CREATE SCHEMA` → schema
- `CREATE TYPE` / `CREATE DOMAIN` → type
- `CREATE SEQUENCE` → sequence
- `CREATE TABLE` (not `CREATE TABLE ... AS`) → table
- `CREATE VIEW` / `CREATE MATERIALIZED VIEW` → view
- `CREATE FUNCTION` / `CREATE PROCEDURE` → function
- `CREATE TRIGGER` → trigger
- `CREATE POLICY` → policy
- `CREATE INDEX` → index (stored with parent table)
- `ALTER TABLE ... ADD CONSTRAINT` → constraint (stored with parent table)
- `GRANT` / `REVOKE` → grant (stored with related object)
- `COMMENT ON` → comment (stored with related object)

### Phase 2: Statement Grouper

**File:** `pkg/migration/group.go`

Group related statements together:

```go
type ObjectFile struct {
    Schema     string
    Type       StatementType
    Name       string
    Statements []string  // Ordered: CREATE, ALTER, GRANT, COMMENT, INDEX, etc.
}

func GroupStatements(statements []ClassifiedStatement) map[string]*ObjectFile
```

For example, a table file would contain:
1. `CREATE TABLE`
2. `ALTER TABLE ... ADD CONSTRAINT` (for the same table)
3. `CREATE INDEX ON table` (indexes on the table)
4. `GRANT ... ON table` (permissions)
5. `COMMENT ON TABLE` / `COMMENT ON COLUMN` (comments)

### Phase 3: Directory Writer

**File:** `pkg/migration/writer.go`

Write grouped statements to the directory structure:

```go
type DumpConfig struct {
    OutputDir     string           // Default: "supabase/schemas"
    Schemas       []string         // Filter to specific schemas
    IncludeRoles  bool             // Include roles dump
    IncludeGrants bool             // Include GRANT statements
}

func WriteStructuredDump(ctx context.Context, config DumpConfig, objects map[string]*ObjectFile, fsys afero.Fs) error
```

File naming conventions:
- Extensions: `00_extensions.sql` (all in one file)
- Roles: `01_roles.sql` (all in one file)
- Schemas: `02_schemas.sql` (all CREATE SCHEMA in one file)
- Types: `{schema}/00_types.sql` (all types per schema in one file)
- Sequences: `{schema}/01_sequences.sql` (all sequences per schema)
- Tables: `{schema}/tables/{table_name}.sql`
- Views: `{schema}/views/{view_name}.sql`
- Functions: `{schema}/functions/{function_name}.sql` (overloads in same file)
- Triggers: `{schema}/triggers/{trigger_name}.sql`
- Policies: `{schema}/policies/{table_name}.sql` (all policies for a table together)

### Phase 4: Command Integration

**File:** `cmd/db.go` and `internal/db/dump/dump.go`

Add new flag to `db dump`:

```
supabase db dump --local --structured [--output-dir ./schemas]
```

**New flags:**
- `--structured` / `-S`: Enable structured directory output
- `--output-dir`: Output directory (default: `supabase/schemas`)

### Phase 5: Config Generator

**File:** `internal/db/dump/config.go`

Generate or update `schema_paths` in config.toml based on dumped structure:

```go
func GenerateSchemaPathsConfig(outputDir string, fsys afero.Fs) ([]string, error)
```

This scans the generated directory structure and produces the recommended glob patterns.

## Detailed Implementation Steps

### Step 1: Statement Classification (2 files)

1. Create `pkg/migration/classify.go`:
   - Regex-based classifier for PostgreSQL DDL statements
   - Extract schema and object names from statements
   - Handle quoted identifiers

2. Create `pkg/migration/classify_test.go`:
   - Test classification of all statement types
   - Test edge cases (quoted names, special characters)

### Step 2: Statement Grouping (2 files)

1. Create `pkg/migration/group.go`:
   - Group statements by schema + object type + object name
   - Order statements within groups correctly
   - Handle cross-references (indexes, constraints, grants)

2. Create `pkg/migration/group_test.go`:
   - Test grouping logic
   - Test statement ordering within groups

### Step 3: Directory Writer (2 files)

1. Create `pkg/migration/writer.go`:
   - Create directory structure
   - Write files with proper headers/separators
   - Handle file naming (sanitize names, handle collisions)
   - Make statements idempotent (IF NOT EXISTS, OR REPLACE)

2. Create `pkg/migration/writer_test.go`:
   - Test file generation
   - Test directory structure creation

### Step 4: Command Changes (2 files)

1. Modify `cmd/db.go`:
   - Add `--structured` and `--output-dir` flags to `db dump`

2. Modify `internal/db/dump/dump.go`:
   - Add `RunStructured()` function
   - Integrate with existing dump pipeline

### Step 5: Config Helper (2 files)

1. Create `internal/db/dump/config.go`:
   - Scan output directory
   - Generate schema_paths config

2. Add to dump output:
   - Print suggested config after structured dump

## Testing Strategy

1. **Unit tests** for each component:
   - Statement classification accuracy
   - Grouping correctness
   - File writing

2. **Integration tests**:
   - Full dump → reload cycle
   - Verify schema matches after apply

3. **Edge cases**:
   - Circular dependencies (handled by idempotent SQL)
   - Special characters in names
   - Very large schemas
   - Empty schemas

## Alternative Considerations

### Alternative 1: Flat Structure
```
schemas/
├── extensions.sql
├── roles.sql
├── public.types.sql
├── public.tables.users.sql
├── public.functions.get_user.sql
```
**Rejected:** Less intuitive navigation, harder to browse

### Alternative 2: By Object Type First
```
schemas/
├── extensions/
├── tables/
│   ├── public.users.sql
│   └── private.secrets.sql
├── functions/
```
**Rejected:** Harder to see all objects in a schema together

### Alternative 3: Single Files per Schema
```
schemas/
├── public.sql
├── private.sql
```
**Rejected:** Large files, harder to diff and review

## Migration Path

For existing users:

1. Run `supabase db dump --structured`
2. Review generated files in `supabase/schemas/`
3. Add `schema_paths` config from generated suggestion
4. Test with `supabase db diff` to verify parity
5. Commit to version control

## Dependencies

- Existing `pkg/parser` for SQL statement splitting
- Existing `pkg/migration/dump.go` for pg_dump execution
- Existing glob pattern system in `pkg/config`

## Success Criteria

1. `db dump --structured` produces correct directory structure
2. Files load correctly via `schema_paths` config
3. `db diff` shows no changes after dump → apply cycle
4. Generated config patterns work with existing glob system
5. Documentation updated with examples
