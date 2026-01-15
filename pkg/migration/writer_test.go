package migration

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteStructuredDump(t *testing.T) {
	fsys := afero.NewMemMapFs()
	ctx := context.Background()

	objects := map[string]*ObjectFile{
		"cluster/roles": {
			Category:   "cluster",
			Name:       "roles",
			Statements: []string{"CREATE ROLE app_user"},
		},
		"schemas/public/schema": {
			Category:   "schemas",
			Schema:     "public",
			Name:       "schema",
			Statements: []string{"CREATE SCHEMA public"},
		},
		"schemas/public/tables/users": {
			Category:   "schemas",
			Schema:     "public",
			Type:       "tables",
			Name:       "users",
			Statements: []string{"CREATE TABLE public.users (id int)"},
		},
	}

	config := StructuredDumpConfig{
		BaseDir:      "supabase",
		IncludeRoles: true,
	}

	err := WriteStructuredDump(ctx, config, objects, fsys)
	require.NoError(t, err)

	// Verify files exist
	exists, _ := afero.Exists(fsys, "supabase/cluster/roles.sql")
	assert.True(t, exists, "roles.sql should exist")

	exists, _ = afero.Exists(fsys, "supabase/schemas/public/schema.sql")
	assert.True(t, exists, "schema.sql should exist")

	exists, _ = afero.Exists(fsys, "supabase/schemas/public/tables/users.sql")
	assert.True(t, exists, "users.sql should exist")

	// Verify content
	content, err := afero.ReadFile(fsys, "supabase/schemas/public/tables/users.sql")
	require.NoError(t, err)
	assert.Equal(t, "CREATE TABLE public.users (id int)\n", string(content))
}

func TestWriteStructuredDumpFilterBySchema(t *testing.T) {
	fsys := afero.NewMemMapFs()
	ctx := context.Background()

	objects := map[string]*ObjectFile{
		"cluster/roles": {
			Category:   "cluster",
			Name:       "roles",
			Statements: []string{"CREATE ROLE app_user"},
		},
		"schemas/public/tables/users": {
			Category:   "schemas",
			Schema:     "public",
			Type:       "tables",
			Name:       "users",
			Statements: []string{"CREATE TABLE public.users"},
		},
		"schemas/private/tables/secrets": {
			Category:   "schemas",
			Schema:     "private",
			Type:       "tables",
			Name:       "secrets",
			Statements: []string{"CREATE TABLE private.secrets"},
		},
	}

	config := StructuredDumpConfig{
		BaseDir:      "supabase",
		Schemas:      []string{"public"},
		IncludeRoles: true,
	}

	err := WriteStructuredDump(ctx, config, objects, fsys)
	require.NoError(t, err)

	// Cluster files should be written
	exists, _ := afero.Exists(fsys, "supabase/cluster/roles.sql")
	assert.True(t, exists)

	// public schema should be written
	exists, _ = afero.Exists(fsys, "supabase/schemas/public/tables/users.sql")
	assert.True(t, exists)

	// private schema should NOT be written
	exists, _ = afero.Exists(fsys, "supabase/schemas/private/tables/secrets.sql")
	assert.False(t, exists, "private schema should not be written")
}

func TestWriteStructuredDumpSkipRoles(t *testing.T) {
	fsys := afero.NewMemMapFs()
	ctx := context.Background()

	objects := map[string]*ObjectFile{
		"cluster/roles": {
			Category:   "cluster",
			Name:       "roles",
			Statements: []string{"CREATE ROLE app_user"},
		},
		"cluster/extensions": {
			Category:   "cluster",
			Name:       "extensions",
			Statements: []string{"CREATE EXTENSION uuid-ossp"},
		},
	}

	config := StructuredDumpConfig{
		BaseDir:      "supabase",
		IncludeRoles: false,
	}

	err := WriteStructuredDump(ctx, config, objects, fsys)
	require.NoError(t, err)

	// Roles should NOT be written
	exists, _ := afero.Exists(fsys, "supabase/cluster/roles.sql")
	assert.False(t, exists, "roles.sql should not exist")

	// Extensions should still be written
	exists, _ = afero.Exists(fsys, "supabase/cluster/extensions.sql")
	assert.True(t, exists)
}

func TestGenerateSchemaPathsConfig(t *testing.T) {
	fsys := afero.NewMemMapFs()

	// Create directory structure
	_ = fsys.MkdirAll("supabase/cluster", 0755)
	_ = fsys.MkdirAll("supabase/schemas/public/tables", 0755)
	_ = afero.WriteFile(fsys, "supabase/cluster/roles.sql", []byte(""), 0644)
	_ = afero.WriteFile(fsys, "supabase/cluster/extensions.sql", []byte(""), 0644)

	paths, err := GenerateSchemaPathsConfig("supabase", fsys)
	require.NoError(t, err)

	assert.Contains(t, paths, "./cluster/roles.sql")
	assert.Contains(t, paths, "./cluster/extensions.sql")
	assert.Contains(t, paths, "./schemas/*/schema.sql")
	assert.Contains(t, paths, "./schemas/*/tables/*.sql")
}

func TestFilterBySchemas(t *testing.T) {
	objects := map[string]*ObjectFile{
		"cluster/roles": {Category: "cluster", Name: "roles"},
		"schemas/public/tables/users": {
			Category: "schemas",
			Schema:   "public",
			Type:     "tables",
			Name:     "users",
		},
		"schemas/private/tables/secrets": {
			Category: "schemas",
			Schema:   "private",
			Type:     "tables",
			Name:     "secrets",
		},
	}

	filtered := FilterBySchemas(objects, []string{"public"})

	// Cluster objects should be included
	assert.Contains(t, filtered, "cluster/roles")

	// public schema should be included
	assert.Contains(t, filtered, "schemas/public/tables/users")

	// private schema should NOT be included
	assert.NotContains(t, filtered, "schemas/private/tables/secrets")
}

func TestFilterBySchemasEmpty(t *testing.T) {
	objects := map[string]*ObjectFile{
		"cluster/roles":                   {Category: "cluster", Name: "roles"},
		"schemas/public/tables/users":     {Category: "schemas", Schema: "public"},
		"schemas/private/tables/secrets":  {Category: "schemas", Schema: "private"},
	}

	// Empty filter should return all objects
	filtered := FilterBySchemas(objects, []string{})
	assert.Len(t, filtered, 3)
}

func TestListSchemas(t *testing.T) {
	objects := map[string]*ObjectFile{
		"cluster/roles": {Category: "cluster", Name: "roles"},
		"schemas/public/tables/users": {
			Category: "schemas",
			Schema:   "public",
		},
		"schemas/api/functions/get_user": {
			Category: "schemas",
			Schema:   "api",
		},
		"schemas/public/schema": {
			Category: "schemas",
			Schema:   "public",
		},
	}

	schemas := ListSchemas(objects)
	assert.Len(t, schemas, 2)
	assert.Contains(t, schemas, "public")
	assert.Contains(t, schemas, "api")
}

func TestSummary(t *testing.T) {
	objects := map[string]*ObjectFile{
		"cluster/roles":                   {Category: "cluster", Name: "roles", Statements: []string{"stmt"}},
		"schemas/public/schema":           {Category: "schemas", Schema: "public", Name: "schema", Statements: []string{"stmt"}},
		"schemas/public/tables/users":     {Category: "schemas", Schema: "public", Type: "tables", Name: "users", Statements: []string{"stmt"}},
		"schemas/public/views/active":     {Category: "schemas", Schema: "public", Type: "views", Name: "active", Statements: []string{"stmt"}},
		"schemas/public/functions/get_id": {Category: "schemas", Schema: "public", Type: "functions", Name: "get_id", Statements: []string{"stmt"}},
	}

	summary := Summary(objects)

	assert.Contains(t, summary, "Schemas: 1")
	assert.Contains(t, summary, "Tables: 1")
	assert.Contains(t, summary, "Views: 1")
	assert.Contains(t, summary, "Functions: 1")
}

func TestPrintSchemaPathsConfig(t *testing.T) {
	var buf bytes.Buffer
	paths := []string{
		"./cluster/roles.sql",
		"./schemas/*/tables/*.sql",
	}

	PrintSchemaPathsConfig(&buf, paths)
	output := buf.String()

	assert.Contains(t, output, "[db.migrations]")
	assert.Contains(t, output, "schema_paths = [")
	assert.Contains(t, output, `"./cluster/roles.sql"`)
	assert.Contains(t, output, `"./schemas/*/tables/*.sql"`)
}

func TestSanitizeFileName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal_name", "normal_name"},
		{"name/with/slashes", "name_with_slashes"},
		{"name:with:colons", "name_with_colons"},
		{"name*with*asterisks", "name_with_asterisks"},
		{"name?with?questions", "name_with_questions"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, SanitizeFileName(tt.input))
		})
	}
}

func TestEnsureBaseDir(t *testing.T) {
	fsys := afero.NewMemMapFs()

	err := EnsureBaseDir("supabase", fsys)
	require.NoError(t, err)

	exists, _ := afero.DirExists(fsys, "supabase/cluster")
	assert.True(t, exists)

	exists, _ = afero.DirExists(fsys, "supabase/schemas")
	assert.True(t, exists)
}

func TestCleanOutputDir(t *testing.T) {
	fsys := afero.NewMemMapFs()

	// Create some existing files
	_ = fsys.MkdirAll("supabase/cluster", 0755)
	_ = fsys.MkdirAll("supabase/schemas/public", 0755)
	_ = afero.WriteFile(fsys, "supabase/cluster/old.sql", []byte("old"), 0644)
	_ = afero.WriteFile(fsys, "supabase/schemas/public/old.sql", []byte("old"), 0644)

	err := CleanOutputDir("supabase", fsys)
	require.NoError(t, err)

	exists, _ := afero.DirExists(fsys, "supabase/cluster")
	assert.False(t, exists)

	exists, _ = afero.DirExists(fsys, "supabase/schemas")
	assert.False(t, exists)
}

func TestWriteToStdout(t *testing.T) {
	var buf bytes.Buffer
	objects := map[string]*ObjectFile{
		"cluster/roles": {
			Category:   "cluster",
			Name:       "roles",
			Statements: []string{"CREATE ROLE app_user"},
		},
	}

	err := WriteToStdout(objects, &buf)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "-- File: cluster/roles.sql")
	assert.Contains(t, output, "CREATE ROLE app_user")
}
