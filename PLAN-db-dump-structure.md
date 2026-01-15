# Plan: Structured Database Dump for Declarative Schemas

## Implementation Status: ✅ Complete

**Branch:** `claude/plan-db-dump-structure-Msf9h`

### Files Created
- `pkg/migration/classify.go` - Statement classifier using pg_query_go
- `pkg/migration/classify_test.go` - Tests for classifier
- `pkg/migration/group.go` - Statement grouper
- `pkg/migration/group_test.go` - Tests for grouper
- `pkg/migration/writer.go` - Directory writer and config generator
- `pkg/migration/writer_test.go` - Tests for writer

### Files Modified
- `cmd/db.go` - Added `--structured` and `--output-dir` flags
- `internal/db/dump/dump.go` - Added `RunStructured()` function
- `go.mod` - Added `pg_query_go/v5` dependency

---

## Testing Instructions

### Prerequisites
1. Have a Supabase project with a local database running
2. Build the CLI from source

### Build from Source
```bash
cd /path/to/cli
go build -o supabase-dev .
```

### Test Commands

**1. Basic structured dump (from local database):**
```bash
cd /path/to/your-supabase-project
/path/to/supabase-dev db dump --local --structured
```

This will:
- Create `supabase/cluster/` directory with cluster-wide objects
- Create `supabase/schemas/{schema}/` directories with schema-scoped objects
- Print a summary of files written
- Print suggested `schema_paths` config for `config.toml`

**2. Dump specific schemas only:**
```bash
/path/to/supabase-dev db dump --local --structured --schema public,auth
```

**3. Dump to custom directory:**
```bash
/path/to/supabase-dev db dump --local --structured --output-dir ./my-schemas
```

**4. Dump from linked remote project:**
```bash
/path/to/supabase-dev db dump --linked --structured
```

### Verification Steps

1. **Check directory structure:**
   ```bash
   tree supabase/cluster supabase/schemas
   ```

2. **Verify file contents:**
   ```bash
   cat supabase/schemas/public/tables/your_table.sql
   ```
   - Should contain CREATE TABLE + indexes + constraints + policies + triggers

3. **Test round-trip (dump → apply → diff):**
   ```bash
   # 1. Dump current schema
   /path/to/supabase-dev db dump --local --structured

   # 2. Add the suggested schema_paths to config.toml

   # 3. Reset database and apply
   /path/to/supabase-dev db reset

   # 4. Verify no diff
   /path/to/supabase-dev db diff --local
   # Should show no changes
   ```

### Run Unit Tests
```bash
go test ./pkg/migration/... -v
```

---

## Key Decisions Made

1. **Parser Choice: pg_query_go over multigres**
   - Used `github.com/pganalyze/pg_query_go/v5` for AST-based SQL parsing
   - Well-established library used by many PostgreSQL tools
   - Provides comprehensive AST access for all PostgreSQL statement types
   - Note: Requires cgo (links to libpg_query)

2. **Single roles.sql file** (not one file per role)
   - Role grants (`GRANT role TO role`) logically belong with both roles
   - Keeping all roles in one file makes the hierarchy visible

3. **Policies grouped with tables** (not separate directory)
   - RLS policies are tightly coupled to their tables
   - Makes table files self-contained for security review

4. **Triggers grouped with target objects**
   - BEFORE/AFTER triggers → grouped with their table
   - INSTEAD OF triggers → grouped with their view
   - Trigger functions (`RETURNS TRIGGER`) → grouped with the trigger that uses them

5. **No numeric prefixes**
   - Ordering controlled via `schema_paths` config
   - Cleaner, more readable filenames

6. **Cluster-wide objects separate from schemas**
   - Reflects PostgreSQL's actual hierarchy
   - `cluster/` directory for: roles, extensions, FDWs, publications, subscriptions, event triggers

---

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

Separate cluster-wide objects from schema-scoped objects:

```
supabase/
├── cluster/
│   ├── roles.sql                  # All custom roles and role grants
│   ├── extensions.sql             # CREATE EXTENSION statements
│   ├── foreign_data_wrappers.sql  # FDWs and foreign servers
│   ├── publications.sql           # Logical replication publications
│   ├── subscriptions.sql          # Logical replication subscriptions
│   └── event_triggers.sql         # Database-level event triggers
│
├── schemas/
│   ├── public/                    # One directory per schema
│   │   ├── schema.sql             # CREATE SCHEMA + schema-level grants
│   │   ├── types.sql              # Composite types, enums, domains
│   │   ├── sequences.sql          # Sequences
│   │   ├── tables/
│   │   │   ├── users.sql          # Table + indexes + constraints + policies + triggers + grants + comments
│   │   │   └── posts.sql
│   │   ├── views/
│   │   │   └── user_posts.sql     # View + INSTEAD OF triggers + grants + comments
│   │   ├── materialized_views/
│   │   │   └── user_stats.sql
│   │   ├── functions/
│   │   │   └── get_user.sql       # Scalar and table-valued functions only
│   │   ├── procedures/
│   │   │   └── process_order.sql
│   │   └── foreign_tables/
│   │       └── external_users.sql
│   │
│   ├── private/                   # Additional schemas
│   │   └── ... (same structure)
│   │
│   └── api/                       # API schema example
│       └── ...
│
├── migrations/                    # Existing migrations
└── config.toml
```

### Complete Object Categories

#### Cluster-Wide Objects (`cluster/`)

| File | PostgreSQL Objects |
|------|-------------------|
| `roles.sql` | `CREATE ROLE`, `ALTER ROLE`, `GRANT role TO role` |
| `extensions.sql` | `CREATE EXTENSION` |
| `foreign_data_wrappers.sql` | `CREATE FOREIGN DATA WRAPPER`, `CREATE SERVER`, `CREATE USER MAPPING` |
| `publications.sql` | `CREATE PUBLICATION` |
| `subscriptions.sql` | `CREATE SUBSCRIPTION` |
| `event_triggers.sql` | `CREATE EVENT TRIGGER` |

#### Schema-Scoped Objects (inside `schemas/{schema}/`)

| Directory | File(s) | PostgreSQL Objects |
|-----------|---------|-------------------|
| `schemas/{schema}/` | `schema.sql` | `CREATE SCHEMA`, schema-level `GRANT` |
| `schemas/{schema}/` | `types.sql` | `CREATE TYPE` (enum, composite, range), `CREATE DOMAIN` |
| `schemas/{schema}/` | `sequences.sql` | `CREATE SEQUENCE` |
| `schemas/{schema}/tables/` | `{table}.sql` | `CREATE TABLE`, `ALTER TABLE` (constraints), `CREATE INDEX`, `CREATE POLICY`, BEFORE/AFTER `CREATE TRIGGER`, trigger functions, table `GRANT`, `COMMENT` |
| `schemas/{schema}/views/` | `{view}.sql` | `CREATE VIEW`, INSTEAD OF `CREATE TRIGGER`, trigger functions, view `GRANT`, `COMMENT` |
| `schemas/{schema}/materialized_views/` | `{mview}.sql` | `CREATE MATERIALIZED VIEW`, indexes, `GRANT`, `COMMENT` |
| `schemas/{schema}/functions/` | `{function}.sql` | `CREATE FUNCTION` (scalar/table-valued, all overloads), `GRANT`, `COMMENT` |
| `schemas/{schema}/procedures/` | `{procedure}.sql` | `CREATE PROCEDURE`, `GRANT`, `COMMENT` |
| `schemas/{schema}/foreign_tables/` | `{ftable}.sql` | `CREATE FOREIGN TABLE`, `GRANT` |

### Managing Dependencies via Config

Schema files are run in lexicographic order by default. For projects with dependencies, the `schema_paths` config provides explicit ordering control.

**Example: Simple project (default ordering)**
```toml
[db.migrations]
schema_paths = [
  "./cluster/*.sql",
  "./schemas/**/*.sql",
]
```

**Example: Project with full dependency ordering**
```toml
[db.migrations]
schema_paths = [
  # 1. Cluster-level objects
  "./cluster/roles.sql",
  "./cluster/extensions.sql",
  "./cluster/foreign_data_wrappers.sql",

  # 2. Schema definitions
  "./schemas/*/schema.sql",

  # 3. Types and sequences (before tables)
  "./schemas/*/types.sql",
  "./schemas/*/sequences.sql",

  # 4. Tables (with indexes, constraints)
  "./schemas/*/tables/*.sql",

  # 5. Foreign tables
  "./schemas/*/foreign_tables/*.sql",

  # 6. Views and materialized views (depend on tables)
  "./schemas/*/views/*.sql",
  "./schemas/*/materialized_views/*.sql",

  # 7. Functions and procedures (scalar/table-valued only; trigger functions are with their tables/views)
  "./schemas/*/functions/*.sql",
  "./schemas/*/procedures/*.sql",

  # 8. Replication (depends on tables)
  "./cluster/publications.sql",
  "./cluster/subscriptions.sql",

  # 9. Event triggers (last)
  "./cluster/event_triggers.sql",
]
```

**Example: Specific table ordering for foreign keys**
```toml
[db.migrations]
schema_paths = [
  "./cluster/*.sql",
  "./schemas/*/schema.sql",
  "./schemas/*/types.sql",
  "./schemas/*/sequences.sql",
  "./schemas/public/tables/employees.sql",  # Parent table first
  "./schemas/public/tables/departments.sql",
  "./schemas/public/tables/*.sql",          # Remaining tables (deduped)
  "./schemas/**/*.sql",                     # Everything else
]
```

### Rationale for Structure

1. **Cluster vs Schema separation** - Reflects PostgreSQL's actual hierarchy
2. **No numeric prefixes** - Ordering handled via `schema_paths` config
3. **Clean, descriptive names** - Easier to read and navigate
4. **All roles together** - Role grants (`GRANT role TO role`) stay with both roles, easier to see hierarchy
5. **Grouped small objects** - Types, sequences, roles in single files (less clutter)
6. **Related statements together** - Table file includes its indexes, constraints, policies, triggers, grants
7. **Triggers with targets** - Trigger functions (`RETURNS TRIGGER`) are specialized and belong with their trigger; BEFORE/AFTER triggers go with tables, INSTEAD OF triggers go with views

## Implementation Plan

### Phase 1: SQL Statement Classifier

**File:** `pkg/migration/classify.go`

Create a statement classifier using a PostgreSQL parser (e.g., `multigres` or `pg_query_go`) rather than regex for accurate parsing:

**Why use a real PostgreSQL parser:**
- 100% accurate parsing of all PostgreSQL syntax
- Handles edge cases: quoted identifiers, dollar-quoting, complex expressions
- AST node types map directly to our categories
- Extract schema/object names directly from parse tree
- Future-proof for new PostgreSQL syntax

**Example using multigres:**
```go
import (
    "github.com/multigres/multigres/go/parser"
    "github.com/multigres/multigres/go/parser/ast"
)

func ClassifyStatement(sql string) ClassifiedStatement {
    stmts, err := parser.ParseSQL(sql)
    if err != nil {
        return ClassifiedStatement{Type: TypeOther, Statement: sql}
    }

    switch node := stmts[0].(type) {
    case *ast.CreateStmt:
        return ClassifiedStatement{
            Type:       TypeTable,
            Schema:     node.Relation.Schemaname,
            ObjectName: node.Relation.Relname,
            Statement:  sql,
        }
    case *ast.CreateFunctionStmt:
        // Check return type for TRIGGER
        if isTriggerFunction(node) {
            return ClassifiedStatement{Type: TypeTriggerFunction, ...}
        }
        return ClassifiedStatement{Type: TypeFunction, ...}
    case *ast.CreateTrigStmt:
        // Check timing for INSTEAD OF vs BEFORE/AFTER
        ...
    // ... other cases
    }
}
```

**Statement types to classify:

```go
type StatementType string

const (
    // Cluster-level
    TypeRole               StatementType = "role"
    TypeExtension          StatementType = "extension"
    TypeForeignDataWrapper StatementType = "foreign_data_wrapper"
    TypeForeignServer      StatementType = "foreign_server"
    TypeUserMapping        StatementType = "user_mapping"
    TypePublication        StatementType = "publication"
    TypeSubscription       StatementType = "subscription"
    TypeEventTrigger       StatementType = "event_trigger"

    // Schema-level
    TypeSchema           StatementType = "schema"
    TypeType             StatementType = "type"      // ENUM, COMPOSITE, RANGE, DOMAIN
    TypeSequence         StatementType = "sequence"
    TypeTable            StatementType = "table"
    TypeForeignTable     StatementType = "foreign_table"
    TypeView             StatementType = "view"
    TypeMaterializedView StatementType = "materialized_view"
    TypeFunction         StatementType = "function"
    TypeProcedure        StatementType = "procedure"
    TypeTrigger          StatementType = "trigger"          // BEFORE/AFTER → table, INSTEAD OF → view
    TypeTriggerFunction  StatementType = "trigger_function" // RETURNS TRIGGER functions
    TypePolicy           StatementType = "policy"
    TypeIndex            StatementType = "index"
    TypeConstraint       StatementType = "constraint"
    TypeGrant            StatementType = "grant"
    TypeComment          StatementType = "comment"
    TypeOther            StatementType = "other"
)

type ClassifiedStatement struct {
    Type       StatementType
    Schema     string    // e.g., "public" (empty for cluster-level)
    ObjectName string    // e.g., "users"
    ParentName string    // e.g., table name for index/trigger/policy
    Statement  string    // Full SQL statement
}

func ClassifyStatement(sql string) ClassifiedStatement
```

**AST node type mapping:**

| AST Node Type | Statement Type | Notes |
|---------------|----------------|-------|
| `CreateRoleStmt`, `AlterRoleStmt`, `GrantRoleStmt` | role | |
| `CreateExtensionStmt` | extension | |
| `CreateFdwStmt` | foreign_data_wrapper | |
| `CreateForeignServerStmt` | foreign_server | |
| `CreateUserMappingStmt` | user_mapping | |
| `CreatePublicationStmt` | publication | |
| `CreateSubscriptionStmt` | subscription | |
| `CreateEventTrigStmt` | event_trigger | |
| `CreateSchemaStmt` | schema | |
| `CreateEnumStmt`, `CreateRangeStmt`, `CompositeTypeStmt`, `CreateDomainStmt` | type | |
| `CreateSeqStmt` | sequence | |
| `CreateStmt` | table | Check `relkind` not foreign table |
| `CreateForeignTableStmt` | foreign_table | |
| `ViewStmt` | view | |
| `CreateTableAsStmt` (materialized) | materialized_view | Check `relkind` |
| `CreateFunctionStmt` | function or trigger_function | Check `RETURNS TRIGGER` in return type |
| `CreateProcedureStmt` | procedure | |
| `CreateTrigStmt` | trigger | Check `timing` for INSTEAD OF vs BEFORE/AFTER |
| `CreatePolicyStmt` | policy | |
| `IndexStmt` | index | Extract table name from `relation` |
| `AlterTableStmt` (ADD CONSTRAINT) | constraint | Extract table name |
| `GrantStmt` | grant | Extract object type and name |
| `CommentStmt` | comment | Extract object type and name |

### Phase 2: Statement Grouper

**File:** `pkg/migration/group.go`

Group related statements together:

```go
type ObjectFile struct {
    Category   string           // "cluster", "roles", or "schemas"
    Schema     string           // Empty for cluster-level
    Type       StatementType
    Name       string
    Statements []string         // Ordered statements
}

func GroupStatements(statements []ClassifiedStatement) map[string]*ObjectFile
```

**Grouping rules:**
- Table file: `CREATE TABLE` + constraints + indexes + policies + BEFORE/AFTER triggers + trigger functions + grants + comments
- View file: `CREATE VIEW` + INSTEAD OF triggers + trigger functions + grants + comments
- Function file: All overloads of same function (scalar/table-valued only, not trigger functions) + grants + comments
- Role file: `CREATE ROLE` + `ALTER ROLE` + `GRANT role TO`

**Trigger function association:**
- Trigger functions (`RETURNS TRIGGER`) are grouped with the trigger that references them
- The trigger is grouped with its target table or view
- This keeps the complete trigger definition (function + trigger) with its target object

### Phase 3: Directory Writer

**File:** `pkg/migration/writer.go`

Write grouped statements to the directory structure:

```go
type StructuredDumpConfig struct {
    BaseDir       string           // Default: "supabase"
    Schemas       []string         // Filter to specific schemas (empty = all)
    IncludeRoles  bool             // Include roles dump
    IncludeGrants bool             // Include GRANT statements with objects
}

func WriteStructuredDump(ctx context.Context, config StructuredDumpConfig, objects map[string]*ObjectFile, fsys afero.Fs) error
```

**File paths:**

| Object Type | Path |
|-------------|------|
| Role | `cluster/roles.sql` |
| Extension | `cluster/extensions.sql` |
| FDW | `cluster/foreign_data_wrappers.sql` |
| Foreign Server | `cluster/foreign_data_wrappers.sql` |
| User Mapping | `cluster/foreign_data_wrappers.sql` |
| Publication | `cluster/publications.sql` |
| Subscription | `cluster/subscriptions.sql` |
| Event Trigger | `cluster/event_triggers.sql` |
| Schema | `schemas/{schema}/schema.sql` |
| Type/Domain | `schemas/{schema}/types.sql` |
| Sequence | `schemas/{schema}/sequences.sql` |
| Table | `schemas/{schema}/tables/{table}.sql` (includes BEFORE/AFTER triggers + trigger functions) |
| Foreign Table | `schemas/{schema}/foreign_tables/{table}.sql` |
| View | `schemas/{schema}/views/{view}.sql` (includes INSTEAD OF triggers + trigger functions) |
| Materialized View | `schemas/{schema}/materialized_views/{mview}.sql` |
| Function | `schemas/{schema}/functions/{function}.sql` (scalar/table-valued only) |
| Procedure | `schemas/{schema}/procedures/{procedure}.sql` |

### Phase 4: Command Integration

**File:** `cmd/db.go` and `internal/db/dump/dump.go`

Add new flag to `db dump`:

```
supabase db dump --local --structured [--output-dir .]
```

**New flags:**
- `--structured` / `-S`: Enable structured directory output
- `--output-dir`: Base output directory (default: current supabase dir)

### Phase 5: Config Generator

**File:** `internal/db/dump/config.go`

Generate recommended `schema_paths` config:

```go
func GenerateSchemaPathsConfig(baseDir string, fsys afero.Fs) ([]string, error)
```

Scans directory structure and produces ordered glob patterns.

## Detailed Implementation Steps

### Step 1: Statement Classification (2 files)

1. Create `pkg/migration/classify.go`:
   - Use PostgreSQL parser (multigres or pg_query_go) for AST-based classification
   - Switch on AST node types to determine statement category
   - Extract schema, object name, and parent references from AST nodes
   - Handle all PostgreSQL syntax correctly (quoted identifiers, dollar-quoting, etc.)

2. Create `pkg/migration/classify_test.go`:
   - Test classification of all statement types
   - Test edge cases (quoted names, special characters, complex expressions)
   - Verify schema/object name extraction from various syntax forms

### Step 2: Statement Grouping (2 files)

1. Create `pkg/migration/group.go`:
   - Group statements by category + schema + type + name
   - Order statements within groups
   - Handle parent references (indexes → table, triggers → table)

2. Create `pkg/migration/group_test.go`:
   - Test grouping logic
   - Test parent association

### Step 3: Directory Writer (2 files)

1. Create `pkg/migration/writer.go`:
   - Create directory structure
   - Write files with proper formatting
   - Handle file naming (sanitize, collisions)
   - Make statements idempotent where possible

2. Create `pkg/migration/writer_test.go`:
   - Test file generation
   - Test directory structure

### Step 4: Command Changes (2 files)

1. Modify `cmd/db.go`:
   - Add `--structured` and `--output-dir` flags

2. Modify `internal/db/dump/dump.go`:
   - Add `RunStructured()` function
   - Integrate with existing pipeline

### Step 5: Config Helper (2 files)

1. Create `internal/db/dump/config.go`:
   - Scan output directory
   - Generate schema_paths config

2. Print suggested config after dump

### Step 6: Update Config Loading

1. Modify `pkg/config/config.go`:
   - Update path resolution to handle `roles/` and `cluster/` directories
   - Ensure relative paths work from supabase directory

## Example Output

After running `supabase db dump --structured`:

```
supabase/
├── cluster/
│   ├── roles.sql
│   ├── extensions.sql
│   └── foreign_data_wrappers.sql
└── schemas/
    └── public/
        ├── schema.sql
        ├── types.sql
        ├── sequences.sql
        ├── tables/
        │   ├── employees.sql
        │   └── managers.sql
        ├── views/
        │   └── profiles.sql
        └── functions/
            └── get_age.sql
```

**Generated `cluster/roles.sql`:**
```sql
CREATE ROLE "app_user" WITH NOLOGIN;
CREATE ROLE "app_admin" WITH NOLOGIN;

GRANT "app_user" TO "app_admin";

GRANT USAGE ON SCHEMA "public" TO "app_user";
GRANT ALL ON SCHEMA "public" TO "app_admin";
```

**Generated `cluster/extensions.sql`:**
```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
```

**Generated `cluster/foreign_data_wrappers.sql`:**
```sql
CREATE FOREIGN DATA WRAPPER "postgres_fdw"
  HANDLER postgres_fdw_handler
  VALIDATOR postgres_fdw_validator;

CREATE SERVER "external_db"
  FOREIGN DATA WRAPPER "postgres_fdw"
  OPTIONS (host 'db.example.com', dbname 'external');

CREATE USER MAPPING FOR "postgres"
  SERVER "external_db"
  OPTIONS (user 'remote_user');
```

**Generated `schemas/public/schema.sql`:**
```sql
CREATE SCHEMA IF NOT EXISTS "public";

GRANT USAGE ON SCHEMA "public" TO "app_user";
GRANT ALL ON SCHEMA "public" TO "app_admin";
```

**Generated `schemas/public/tables/employees.sql`:**
```sql
CREATE TABLE IF NOT EXISTS "public"."employees" (
  "id" integer NOT NULL,
  "name" text,
  "department_id" integer,
  "age" smallint NOT NULL,
  "updated_at" timestamptz DEFAULT now(),
  CONSTRAINT "employees_pkey" PRIMARY KEY ("id")
);

ALTER TABLE "public"."employees"
  ADD CONSTRAINT "employees_department_fkey"
  FOREIGN KEY ("department_id") REFERENCES "public"."departments"("id");

CREATE INDEX "employees_name_idx" ON "public"."employees" ("name");

ALTER TABLE "public"."employees" ENABLE ROW LEVEL SECURITY;

CREATE POLICY "employees_select_policy" ON "public"."employees"
  FOR SELECT TO "app_user"
  USING (true);

CREATE POLICY "employees_all_policy" ON "public"."employees"
  FOR ALL TO "app_admin"
  USING (true);

-- Trigger function and trigger defined together with the table
CREATE OR REPLACE FUNCTION "public"."employees_update_timestamp"()
  RETURNS TRIGGER
  LANGUAGE plpgsql
AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$;

CREATE TRIGGER "employees_update_timestamp_trigger"
  BEFORE UPDATE ON "public"."employees"
  FOR EACH ROW
  EXECUTE FUNCTION "public"."employees_update_timestamp"();

GRANT SELECT ON "public"."employees" TO "app_user";
GRANT ALL ON "public"."employees" TO "app_admin";

COMMENT ON TABLE "public"."employees" IS 'Company employees';
COMMENT ON COLUMN "public"."employees"."age" IS 'Employee age in years';
```

**Suggested config output:**
```
Structured dump complete. Add to config.toml:

[db.migrations]
schema_paths = [
  "./cluster/roles.sql",
  "./cluster/extensions.sql",
  "./cluster/foreign_data_wrappers.sql",
  "./schemas/*/schema.sql",
  "./schemas/*/types.sql",
  "./schemas/*/sequences.sql",
  "./schemas/*/tables/*.sql",
  "./schemas/*/foreign_tables/*.sql",
  "./schemas/*/views/*.sql",
  "./schemas/*/materialized_views/*.sql",
  "./schemas/*/functions/*.sql",
  "./schemas/*/procedures/*.sql",
  "./cluster/publications.sql",
  "./cluster/subscriptions.sql",
  "./cluster/event_triggers.sql",
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
   - Empty directories (not created)
   - Overloaded functions
   - Tables with many policies

## Alternative Considerations

### Alternative 1: Extensions in schemas/
**Rejected:** Extensions are cluster-wide, not schema-scoped (even though they may create objects in a schema)

### Alternative 2: One file per role
**Rejected:** Role grants (`GRANT role TO role`) logically belong with both roles; single file keeps hierarchy visible

### Alternative 3: Triggers in separate files
**Rejected:** Trigger functions (`RETURNS TRIGGER`) can only be used as triggers, so they belong with their target table/view. INSTEAD OF triggers only work on views. Keeping trigger + trigger function together with the target object is more cohesive.

### Alternative 4: Numeric prefixes for ordering
**Rejected:** Ordering should be controlled via config, not filename conventions

## Migration Path

For existing users:

1. Run `supabase db dump --structured`
2. Review generated files in `supabase/`
3. Add `schema_paths` config from generated suggestion
4. Customize ordering if needed for specific dependencies
5. Test with `supabase db diff` to verify parity
6. Commit to version control

## Code Changes Required

### Files to Create
- `pkg/migration/classify.go`
- `pkg/migration/classify_test.go`
- `pkg/migration/group.go`
- `pkg/migration/group_test.go`
- `pkg/migration/writer.go`
- `pkg/migration/writer_test.go`
- `internal/db/dump/config.go`

### Files to Modify
- `cmd/db.go` - Add flags
- `internal/db/dump/dump.go` - Add structured dump function
- `pkg/config/config.go` - Support `cluster/` directory path
- `internal/utils/misc.go` - Add path constant for `cluster/` directory

## Dependencies

- **PostgreSQL parser** (`multigres` or `pg_query_go`) for AST-based statement classification
- Existing `pkg/parser` for SQL statement splitting (still used to split pg_dump output into individual statements)
- Existing `pkg/migration/dump.go` for pg_dump execution
- Existing glob pattern system in `pkg/config`

### Parser Options

| Library | Pros | Cons |
|---------|------|------|
| `multigres` | Pure Go, no cgo | Newer, less battle-tested |
| `pg_query_go` | Well-established, used by many tools | Requires cgo (links to libpg_query) |

Recommendation: Evaluate both; prefer pure Go solution if feature-complete for our needs.

## Success Criteria

1. `db dump --structured` produces correct directory structure
2. Cluster-wide objects are outside `schemas/` directory
3. Files load correctly via `schema_paths` config
4. `db diff` shows no changes after dump → apply cycle
5. Generated config patterns work with existing glob system
6. Documentation updated with examples
