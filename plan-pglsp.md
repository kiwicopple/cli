# Postgres Language Server Integration

## Overview

This document outlines a proposal to integrate the [Postgres Language Server](https://pg-language-server.com/) (pglsp) into the Supabase CLI for robust SQL parsing, validation, and linting.

**GitHub:** https://github.com/supabase-community/postgres-language-server

## Related Documents

- [Database Hot Reloading](./plan.md) - The main feature that will use pglsp for SQL validation

---

## Why Use pglsp?

| Current Approach | With pglsp |
|------------------|------------|
| Simple string matching for destructive ops | Full AST-based SQL parsing |
| No syntax validation before apply | Catch syntax errors instantly on save |
| No type checking | Type-check queries against schema |
| Basic keyword detection | Proper SQL linting (Squawk-inspired rules) |

### Key Benefits

- **100% syntax compatibility** - Built on `libpg_query`, Postgres' actual parser
- **Transport-agnostic design** - Can be used via CLI, HTTP API, or WebAssembly
- **Already in Supabase ecosystem** - Maintained by supabase-community
- **MIT licensed** - Free to embed and distribute

---

## pglsp Capabilities

1. **Syntax Error Detection** - Catch SQL errors before hitting the database
2. **Type Checking** - Validate column types, table references via EXPLAIN
3. **Autocompletion** - Could enhance future IDE integrations
4. **Linting** - Enforce SQL best practices (inspired by Squawk)

---

## Embedding Options

### Option 1: WebAssembly (Recommended)

Embed pglsp as a WASM module directly in the CLI.

**Pros:**
- No external dependencies at runtime
- Fast, in-process execution
- Single binary distribution
- Works offline

**Cons:**
- Increases binary size
- Requires WASM runtime in Go (e.g., wazero, wasmtime-go)

**Implementation:**

```go
// internal/db/serve/validator.go
package serve

import (
    "context"
    "github.com/tetratelabs/wazero"
)

type SQLValidator struct {
    runtime wazero.Runtime
    module  wazero.CompiledModule
}

func NewSQLValidator(ctx context.Context) (*SQLValidator, error) {
    // Initialize WASM runtime
    r := wazero.NewRuntime(ctx)

    // Load pglsp WASM module (embedded via go:embed)
    module, err := r.CompileModule(ctx, pglspWasm)
    if err != nil {
        return nil, err
    }

    return &SQLValidator{runtime: r, module: module}, nil
}

func (v *SQLValidator) Validate(sql string) (*ValidationResult, error) {
    // Call pglsp WASM functions for:
    // 1. Syntax validation
    // 2. Destructive operation detection
    // 3. Linting rules
    return &ValidationResult{}, nil
}

type ValidationResult struct {
    Valid          bool
    SyntaxErrors   []SQLError
    Warnings       []SQLWarning
    IsDestructive  bool
    DestructiveOps []string  // e.g., ["DROP TABLE users", "TRUNCATE orders"]
}
```

**WASM Runtime Options for Go:**

| Library | Pros | Cons |
|---------|------|------|
| [wazero](https://github.com/tetratelabs/wazero) | Pure Go, no CGO, fast | Newer, less mature |
| [wasmtime-go](https://github.com/bytecodealliance/wasmtime-go) | Battle-tested, full WASI | Requires CGO |
| [wasmer-go](https://github.com/wasmerio/wasmer-go) | Good performance | Requires CGO |

**Recommendation:** Use **wazero** for pure Go compilation and simpler cross-platform builds.

---

### Option 2: CLI Subprocess

Shell out to the pglsp CLI binary.

**Pros:**
- Simpler integration
- Easy to update pglsp independently
- No WASM complexity

**Cons:**
- Requires pglsp binary installed or bundled
- Process spawn overhead on each validation
- More complex distribution

**Implementation:**

```go
// internal/db/serve/validator_cli.go
package serve

import (
    "encoding/json"
    "os/exec"
    "strings"
)

type CLIValidator struct {
    binaryPath string
}

func (v *CLIValidator) Validate(sql string) (*ValidationResult, error) {
    cmd := exec.Command(v.binaryPath, "lint", "--format", "json")
    cmd.Stdin = strings.NewReader(sql)

    output, err := cmd.Output()
    if err != nil {
        return nil, err
    }

    var result ValidationResult
    json.Unmarshal(output, &result)
    return &result, nil
}
```

**Binary Distribution Options:**
- Bundle in CLI releases (increases download size)
- Download on first use (like Deno does)
- Require manual installation (document in prerequisites)

---

### Option 3: HTTP Sidecar

Run pglsp as a local HTTP server.

**Pros:**
- Language-agnostic integration
- Could be shared across multiple tools
- Easy to debug/inspect

**Cons:**
- Additional process to manage
- Port conflicts possible
- More complex startup/shutdown

**Implementation:**

```go
// internal/db/serve/validator_http.go
package serve

import (
    "bytes"
    "encoding/json"
    "net/http"
)

type HTTPValidator struct {
    endpoint string // e.g., "http://localhost:7654"
}

func (v *HTTPValidator) Validate(sql string) (*ValidationResult, error) {
    resp, err := http.Post(
        v.endpoint+"/validate",
        "application/json",
        bytes.NewReader([]byte(sql)),
    )
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result ValidationResult
    json.NewDecoder(resp.Body).Decode(&result)
    return &result, nil
}
```

---

## Recommended Approach

### Phase 1: CLI Subprocess (Quick Start)

Start with CLI subprocess for quick integration:
- Bundle pglsp binary in releases
- Simple to implement and test
- Validates the integration approach

### Phase 2: WASM Embedding (Production)

Migrate to WASM embedding for production:
- Better performance (no process spawn)
- Single binary distribution
- Works offline without setup

---

## Integration Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  File Change Detected                                       │
│         ↓                                                   │
│  ┌─────────────────────────────────────────────────────┐   │
│  │  pglsp Validation (WASM/CLI/HTTP)                   │   │
│  │    • Parse SQL (libpg_query)                        │   │
│  │    • Check syntax errors                            │   │
│  │    • Detect destructive operations                  │   │
│  │    • Run linting rules                              │   │
│  └─────────────────────────────────────────────────────┘   │
│         ↓                                                   │
│    Syntax Error? → Show error, wait for fix                │
│         ↓                                                   │
│    Destructive? → Prompt for confirmation                  │
│         ↓                                                   │
│    Apply migration/seed                                     │
└─────────────────────────────────────────────────────────────┘
```

---

## Enhanced User Experience with pglsp

```
$ supabase db serve

Watching for database changes...
  → supabase/migrations/
  → supabase/seed.sql

[12:01:15] Changes detected in: [migrations/20240115_add_users.sql]
[12:01:15] Validating SQL...
[12:01:15] ✗ Syntax error at line 3, column 15:
           CREATE TABEL users (id uuid);
                  ^^^^^
           Did you mean: CREATE TABLE

           Fix the error and save to retry.

[12:01:45] Changes detected in: [migrations/20240115_add_users.sql]
[12:01:45] Validating SQL... ✓
[12:01:45] ⚠️  Lint warning: Consider adding IF NOT EXISTS to CREATE TABLE
[12:01:45] Applying migration: 20240115_add_users.sql
[12:01:46] Migration applied successfully ✓
```

---

## Files to Create

| File | Description |
|------|-------------|
| `internal/db/serve/validator.go` | Validator interface and factory |
| `internal/db/serve/validator_wasm.go` | WASM-based validator |
| `internal/db/serve/validator_cli.go` | CLI subprocess validator |
| `internal/db/serve/pglsp.wasm` | Embedded WASM binary (go:embed) |
| `internal/db/serve/validator_test.go` | Validator tests |

---

## Validator Interface

```go
// internal/db/serve/validator.go
package serve

type SQLError struct {
    Line    int
    Column  int
    Message string
    Hint    string
}

type SQLWarning struct {
    Line    int
    Column  int
    Rule    string
    Message string
}

type ValidationResult struct {
    Valid          bool
    SyntaxErrors   []SQLError
    Warnings       []SQLWarning
    IsDestructive  bool
    DestructiveOps []string
}

type Validator interface {
    Validate(sql string) (*ValidationResult, error)
    Close() error
}

// Factory function to create the appropriate validator
func NewValidator(cfg ValidatorConfig) (Validator, error) {
    switch cfg.Mode {
    case "wasm":
        return NewWASMValidator(cfg)
    case "cli":
        return NewCLIValidator(cfg)
    case "http":
        return NewHTTPValidator(cfg)
    default:
        // Default to CLI for Phase 1
        return NewCLIValidator(cfg)
    }
}
```

---

## Configuration

### config.toml Support

```toml
[db.validation]
enabled = true
mode = "cli"                    # "wasm", "cli", or "http"
lint_enabled = true
lint_rules = ["all"]            # or specific rules to enable
binary_path = ""                # Path to pglsp binary (for cli mode)
http_endpoint = ""              # Endpoint for http mode
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SUPABASE_PGLSP_MODE` | Validator mode | `cli` |
| `SUPABASE_PGLSP_BINARY` | Path to pglsp binary | Auto-detect |
| `SUPABASE_PGLSP_LINT` | Enable linting | `true` |

---

## Testing Plan

### Unit Tests

1. **Syntax Validation**
   - Valid SQL passes validation
   - Invalid SQL returns appropriate errors
   - Error positions are accurate

2. **Destructive Detection**
   - DROP statements detected
   - TRUNCATE statements detected
   - DELETE without WHERE detected
   - Safe statements not flagged

3. **Linting**
   - Lint rules trigger appropriate warnings
   - Lint rules can be disabled

### Integration Tests

1. **WASM Integration**
   - WASM module loads correctly
   - Multiple validations work
   - Memory is properly managed

2. **CLI Integration**
   - Binary is found/downloaded
   - JSON output is parsed correctly
   - Errors are handled gracefully

---

## Rollout Plan

### Phase 1: CLI Integration
- Bundle pglsp binary in releases
- Basic syntax validation
- Destructive operation detection

### Phase 2: WASM Migration
- Embed WASM module
- Remove binary dependency
- Performance optimization

### Phase 3: Advanced Features
- Full linting support
- Custom lint rules
- Schema-aware validation (connect to local DB)

---

## Open Questions

1. **WASM binary size** - How large is the pglsp WASM module? Impact on CLI binary size?

2. **Schema awareness** - Can pglsp connect to local DB for type checking, or is it static analysis only?

3. **Custom lint rules** - Can we configure which linting rules to enable/disable?

4. **Version compatibility** - How to handle pglsp updates? Embed version in CLI releases?

5. **Offline support** - For CLI mode, should we download the binary on first use or require manual install?

---

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| WASM binary too large | Medium | Start with CLI mode, optimize WASM later |
| pglsp API changes | Low | Pin to specific version, test on upgrade |
| Performance overhead | Low | Debouncing, async validation |
| Platform compatibility | Medium | Test on Linux, macOS, Windows |

---

## Success Metrics

- Syntax errors caught before DB apply (should be 100%)
- False positive rate for destructive detection (should be <5%)
- Validation latency (should be <100ms for typical migrations)
- User satisfaction with error messages

---

## References

- [Postgres Language Server](https://pg-language-server.com/)
- [GitHub - supabase-community/postgres-language-server](https://github.com/supabase-community/postgres-language-server)
- [wazero - Go WASM runtime](https://github.com/tetratelabs/wazero)
- [libpg_query](https://github.com/pganalyze/libpg_query) - The underlying Postgres parser
