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
│   │   │   ├── users.sql          # Table + indexes + constraints + policies + grants + comments
│   │   │   └── posts.sql
│   │   ├── views/
│   │   │   └── user_posts.sql
│   │   ├── materialized_views/
│   │   │   └── user_stats.sql
│   │   ├── functions/
│   │   │   └── get_user.sql
│   │   ├── procedures/
│   │   │   └── process_order.sql
│   │   ├── triggers/
│   │   │   └── update_timestamp.sql
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
| `schemas/{schema}/tables/` | `{table}.sql` | `CREATE TABLE`, `ALTER TABLE` (constraints), `CREATE INDEX`, `CREATE POLICY`, table `GRANT`, `COMMENT` |
| `schemas/{schema}/views/` | `{view}.sql` | `CREATE VIEW`, view `GRANT`, `COMMENT` |
| `schemas/{schema}/materialized_views/` | `{mview}.sql` | `CREATE MATERIALIZED VIEW`, indexes, `GRANT`, `COMMENT` |
| `schemas/{schema}/functions/` | `{function}.sql` | `CREATE FUNCTION` (all overloads), `GRANT`, `COMMENT` |
| `schemas/{schema}/procedures/` | `{procedure}.sql` | `CREATE PROCEDURE`, `GRANT`, `COMMENT` |
| `schemas/{schema}/triggers/` | `{trigger}.sql` | `CREATE TRIGGER` |
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

  # 7. Functions and procedures
  "./schemas/*/functions/*.sql",
  "./schemas/*/procedures/*.sql",

  # 8. Triggers (depend on tables and functions)
  "./schemas/*/triggers/*.sql",

  # 9. Replication (depends on tables)
  "./cluster/publications.sql",
  "./cluster/subscriptions.sql",

  # 10. Event triggers (last)
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
6. **Related statements together** - Table file includes its indexes, constraints, policies, grants

## Implementation Plan

### Phase 1: SQL Statement Classifier

**File:** `pkg/migration/classify.go`

Create a statement classifier that parses pg_dump output and categorizes each statement:

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
    TypeTrigger          StatementType = "trigger"
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

**Classification patterns:**

| Pattern | Type |
|---------|------|
| `CREATE ROLE` / `ALTER ROLE` / `GRANT ... TO` | role |
| `CREATE EXTENSION` | extension |
| `CREATE FOREIGN DATA WRAPPER` | foreign_data_wrapper |
| `CREATE SERVER` | foreign_server |
| `CREATE USER MAPPING` | user_mapping |
| `CREATE PUBLICATION` | publication |
| `CREATE SUBSCRIPTION` | subscription |
| `CREATE EVENT TRIGGER` | event_trigger |
| `CREATE SCHEMA` | schema |
| `CREATE TYPE` / `CREATE DOMAIN` | type |
| `CREATE SEQUENCE` | sequence |
| `CREATE TABLE` (not AS SELECT) | table |
| `CREATE FOREIGN TABLE` | foreign_table |
| `CREATE VIEW` | view |
| `CREATE MATERIALIZED VIEW` | materialized_view |
| `CREATE FUNCTION` | function |
| `CREATE PROCEDURE` | procedure |
| `CREATE TRIGGER` | trigger |
| `CREATE POLICY` | policy |
| `CREATE INDEX` | index |
| `ALTER TABLE ... ADD CONSTRAINT` | constraint |
| `GRANT` / `REVOKE` on object | grant |
| `COMMENT ON` | comment |

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
- Table file: `CREATE TABLE` + `ALTER TABLE ADD CONSTRAINT` + `CREATE INDEX ON` + `CREATE POLICY` + `GRANT ON TABLE` + `COMMENT ON TABLE/COLUMN`
- View file: `CREATE VIEW` + `GRANT ON VIEW` + `COMMENT ON VIEW`
- Function file: All overloads of same function + `GRANT ON FUNCTION` + `COMMENT`
- Role file: `CREATE ROLE` + `ALTER ROLE` + `GRANT role TO`

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
| Table | `schemas/{schema}/tables/{table}.sql` |
| Foreign Table | `schemas/{schema}/foreign_tables/{table}.sql` |
| View | `schemas/{schema}/views/{view}.sql` |
| Materialized View | `schemas/{schema}/materialized_views/{mview}.sql` |
| Function | `schemas/{schema}/functions/{function}.sql` |
| Procedure | `schemas/{schema}/procedures/{procedure}.sql` |
| Trigger | `schemas/{schema}/triggers/{trigger}.sql` |

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
   - Regex-based classifier for all PostgreSQL DDL statements
   - Extract schema, object name, and parent references
   - Handle quoted identifiers

2. Create `pkg/migration/classify_test.go`:
   - Test classification of all statement types
   - Test edge cases (quoted names, special characters)

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
  "./schemas/*/triggers/*.sql",
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

### Alternative 3: Triggers with their tables
**Rejected:** Triggers often reference functions; keeping them separate allows proper ordering

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

- Existing `pkg/parser` for SQL statement splitting
- Existing `pkg/migration/dump.go` for pg_dump execution
- Existing glob pattern system in `pkg/config`

## Success Criteria

1. `db dump --structured` produces correct directory structure
2. Cluster-wide objects are outside `schemas/` directory
3. Files load correctly via `schema_paths` config
4. `db diff` shows no changes after dump → apply cycle
5. Generated config patterns work with existing glob system
6. Documentation updated with examples
