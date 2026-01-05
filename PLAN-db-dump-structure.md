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

Mirror PostgreSQL's logical organization with clean, descriptive names:

```
supabase/
├── schemas/
│   ├── extensions.sql          # Extensions (cluster-wide)
│   ├── roles.sql               # Custom roles and grants
│   │
│   ├── public/                 # One directory per schema
│   │   ├── types.sql           # Composite types, enums, domains
│   │   ├── sequences.sql       # Sequences
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

### Managing Dependencies via Config

As documented in the Supabase docs, schema files are run in lexicographic order by default. For projects with dependencies between objects, the `schema_paths` config provides explicit ordering control. Any glob patterns are evaluated, deduplicated, and sorted lexicographically.

**Example: Simple project (default ordering is sufficient)**
```toml
[db.migrations]
schema_paths = ["./schemas/**/*.sql"]
```

**Example: Project with dependencies**
```toml
[db.migrations]
schema_paths = [
  # Cluster-level objects first
  "./schemas/extensions.sql",
  "./schemas/roles.sql",

  # Types and sequences before tables (tables may reference them)
  "./schemas/*/types.sql",
  "./schemas/*/sequences.sql",

  # Tables before views/functions (views query tables)
  "./schemas/*/tables/*.sql",

  # Views and functions
  "./schemas/*/views/*.sql",
  "./schemas/*/functions/*.sql",

  # Triggers reference tables and functions
  "./schemas/*/triggers/*.sql",

  # Policies last (reference tables)
  "./schemas/*/policies/*.sql",
]
```

**Example: Specific table ordering for foreign keys**

When `managers` references `employees`:
```toml
[db.migrations]
schema_paths = [
  "./schemas/extensions.sql",
  "./schemas/public/tables/employees.sql",  # Parent table first
  "./schemas/public/tables/*.sql",          # Remaining tables (deduped)
  "./schemas/**/*.sql",                     # Everything else
]
```

### Rationale for Structure

1. **No numeric prefixes** - Ordering is handled by `schema_paths` config, not filename conventions
2. **Clean, descriptive names** - Easier to read and navigate
3. **Schema-first hierarchy** - Groups all objects for a schema together
4. **Separate files per object** - Easier to review changes, better git history
5. **Aggregated small objects** - Types, sequences grouped per schema (less file clutter)

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
- Extensions: `extensions.sql` (all in one file)
- Roles: `roles.sql` (all in one file)
- Types: `{schema}/types.sql` (all types per schema in one file)
- Sequences: `{schema}/sequences.sql` (all sequences per schema)
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

Generate recommended `schema_paths` config based on dumped structure:

```go
func GenerateSchemaPathsConfig(outputDir string, fsys afero.Fs) ([]string, error)
```

This scans the generated directory structure and produces recommended glob patterns with proper ordering for dependencies.

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
   - Generate schema_paths config with dependency ordering

2. Add to dump output:
   - Print suggested config after structured dump

## Example Output

After running `supabase db dump --structured`:

```
supabase/
└── schemas/
    ├── extensions.sql
    ├── roles.sql
    └── public/
        ├── types.sql
        ├── sequences.sql
        ├── tables/
        │   ├── employees.sql
        │   └── managers.sql
        ├── views/
        │   └── profiles.sql
        ├── functions/
        │   └── get_age.sql
        ├── triggers/
        │   └── update_timestamp.sql
        └── policies/
            └── employees.sql
```

**Generated employees.sql:**
```sql
create table "employees" (
  "id" integer not null,
  "name" text,
  "age" smallint not null
);
```

**Generated profiles.sql (view):**
```sql
create view "profiles" as
  select id, name from "employees";
```

**Generated get_age.sql (function):**
```sql
create function "get_age"(employee_id integer) RETURNS smallint
  LANGUAGE "sql"
AS $$
  select age
  from employees
  where id = employee_id;
$$;
```

**Suggested config output:**
```
Structured dump complete. Add to config.toml:

[db.migrations]
schema_paths = [
  "./schemas/extensions.sql",
  "./schemas/roles.sql",
  "./schemas/*/types.sql",
  "./schemas/*/sequences.sql",
  "./schemas/*/tables/*.sql",
  "./schemas/*/views/*.sql",
  "./schemas/*/functions/*.sql",
  "./schemas/*/triggers/*.sql",
  "./schemas/*/policies/*.sql",
]
```

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

### Alternative 4: Numeric Prefixes
```
schemas/
├── 00_extensions.sql
├── 01_roles.sql
├── public/
│   ├── 00_types.sql
```
**Rejected:** Ordering should be controlled via config, not filename conventions

## Migration Path

For existing users:

1. Run `supabase db dump --structured`
2. Review generated files in `supabase/schemas/`
3. Add `schema_paths` config from generated suggestion
4. Customize ordering if you have specific dependencies
5. Test with `supabase db diff` to verify parity
6. Commit to version control

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
