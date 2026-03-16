package migration

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-errors/errors"
	"github.com/spf13/afero"
)

// StructuredDumpConfig configures the structured dump output
type StructuredDumpConfig struct {
	BaseDir      string   // Base output directory (e.g., "supabase")
	Schemas      []string // Filter to specific schemas (empty = all)
	IncludeRoles bool     // Include roles in dump
}

// DefaultStructuredDumpConfig returns default configuration
func DefaultStructuredDumpConfig() StructuredDumpConfig {
	return StructuredDumpConfig{
		BaseDir:      "supabase",
		IncludeRoles: true,
	}
}

// WriteStructuredDump writes grouped objects to the directory structure
func WriteStructuredDump(ctx context.Context, config StructuredDumpConfig, objects map[string]*ObjectFile, fsys afero.Fs) error {
	for _, key := range SortedKeys(objects) {
		obj := objects[key]

		// Skip empty files
		if len(obj.Statements) == 0 {
			continue
		}

		// Filter by schema if specified
		if len(config.Schemas) > 0 && obj.Category == "schemas" {
			found := false
			for _, s := range config.Schemas {
				if s == obj.Schema {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Skip roles if not included
		if !config.IncludeRoles && obj.Category == "cluster" && obj.Name == "roles" {
			continue
		}

		filePath := filepath.Join(config.BaseDir, obj.FilePath())

		// Create directory
		dir := filepath.Dir(filePath)
		if err := fsys.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("failed to create directory %s: %w", dir, err)
		}

		// Write file
		content := obj.Content()
		if err := afero.WriteFile(fsys, filePath, []byte(content), 0644); err != nil {
			return errors.Errorf("failed to write file %s: %w", filePath, err)
		}
	}

	return nil
}

// GenerateSchemaPathsConfig generates recommended schema_paths config
func GenerateSchemaPathsConfig(baseDir string, fsys afero.Fs) ([]string, error) {
	var paths []string

	// Check which cluster files exist
	clusterFiles := []string{
		"roles.sql",
		"extensions.sql",
		"foreign_data_wrappers.sql",
	}
	for _, f := range clusterFiles {
		path := filepath.Join(baseDir, "cluster", f)
		if exists, _ := afero.Exists(fsys, path); exists {
			paths = append(paths, fmt.Sprintf("./cluster/%s", f))
		}
	}

	// Schema patterns
	schemasDir := filepath.Join(baseDir, "schemas")
	if exists, _ := afero.DirExists(fsys, schemasDir); exists {
		paths = append(paths,
			"./schemas/*/schema.sql",
			"./schemas/*/types.sql",
			"./schemas/*/sequences.sql",
			"./schemas/*/tables/*.sql",
			"./schemas/*/foreign_tables/*.sql",
			"./schemas/*/views/*.sql",
			"./schemas/*/materialized_views/*.sql",
			"./schemas/*/functions/*.sql",
			"./schemas/*/procedures/*.sql",
		)
	}

	// Cluster files that depend on tables
	postTableClusterFiles := []string{
		"publications.sql",
		"subscriptions.sql",
		"event_triggers.sql",
	}
	for _, f := range postTableClusterFiles {
		path := filepath.Join(baseDir, "cluster", f)
		if exists, _ := afero.Exists(fsys, path); exists {
			paths = append(paths, fmt.Sprintf("./cluster/%s", f))
		}
	}

	return paths, nil
}

// PrintSchemaPathsConfig prints the suggested config to a writer
func PrintSchemaPathsConfig(w io.Writer, paths []string) {
	fmt.Fprintln(w, "\nStructured dump complete. Add to config.toml:")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "[db.migrations]")
	fmt.Fprintln(w, "schema_paths = [")
	for _, p := range paths {
		fmt.Fprintf(w, "  %q,\n", p)
	}
	fmt.Fprintln(w, "]")
}

// GetSchemaNames returns a list of schema names from the grouped objects
func GetSchemaNames(objects map[string]*ObjectFile) []string {
	schemaSet := make(map[string]bool)
	for _, obj := range objects {
		if obj.Category == "schemas" && obj.Schema != "" {
			schemaSet[obj.Schema] = true
		}
	}

	schemas := make([]string, 0, len(schemaSet))
	for s := range schemaSet {
		schemas = append(schemas, s)
	}
	return schemas
}

// FilterBySchemas filters objects to only include specified schemas
func FilterBySchemas(objects map[string]*ObjectFile, schemas []string) map[string]*ObjectFile {
	if len(schemas) == 0 {
		return objects
	}

	schemaSet := make(map[string]bool)
	for _, s := range schemas {
		schemaSet[s] = true
	}

	result := make(map[string]*ObjectFile)
	for key, obj := range objects {
		// Always include cluster-level objects
		if obj.Category == "cluster" {
			result[key] = obj
			continue
		}
		// Filter schema-level objects
		if schemaSet[obj.Schema] {
			result[key] = obj
		}
	}
	return result
}

// Summary returns a summary of the objects for display
func Summary(objects map[string]*ObjectFile) string {
	var b strings.Builder

	clusterCount := 0
	schemaCount := 0
	tableCount := 0
	viewCount := 0
	functionCount := 0
	otherCount := 0

	schemas := make(map[string]bool)

	for _, obj := range objects {
		if len(obj.Statements) == 0 {
			continue
		}
		if obj.Category == "cluster" {
			clusterCount++
		} else {
			schemas[obj.Schema] = true
			switch obj.Type {
			case "tables":
				tableCount++
			case "views":
				viewCount++
			case "functions":
				functionCount++
			default:
				if obj.Name == "schema" || obj.Name == "types" || obj.Name == "sequences" {
					schemaCount++
				} else {
					otherCount++
				}
			}
		}
	}

	fmt.Fprintf(&b, "Schemas: %d", len(schemas))
	if clusterCount > 0 {
		fmt.Fprintf(&b, ", Cluster files: %d", clusterCount)
	}
	if tableCount > 0 {
		fmt.Fprintf(&b, ", Tables: %d", tableCount)
	}
	if viewCount > 0 {
		fmt.Fprintf(&b, ", Views: %d", viewCount)
	}
	if functionCount > 0 {
		fmt.Fprintf(&b, ", Functions: %d", functionCount)
	}
	if schemaCount > 0 {
		fmt.Fprintf(&b, ", Other schema files: %d", schemaCount)
	}
	if otherCount > 0 {
		fmt.Fprintf(&b, ", Other: %d", otherCount)
	}

	return b.String()
}

// PrintSummary prints a summary of written files
func PrintSummary(w io.Writer, objects map[string]*ObjectFile) {
	fmt.Fprintf(w, "\n%s\n", Summary(objects))
	fmt.Fprintln(w, "\nFiles written:")

	for _, key := range SortedKeys(objects) {
		obj := objects[key]
		if len(obj.Statements) == 0 {
			continue
		}
		fmt.Fprintf(w, "  %s (%d statements)\n", obj.FilePath(), len(obj.Statements))
	}
}

// WriteToStdout writes all grouped content to stdout in order
func WriteToStdout(objects map[string]*ObjectFile, w io.Writer) error {
	for _, key := range SortedKeys(objects) {
		obj := objects[key]
		if len(obj.Statements) == 0 {
			continue
		}
		fmt.Fprintf(w, "-- File: %s\n", obj.FilePath())
		fmt.Fprintln(w, obj.Content())
	}
	return nil
}

// EnsureBaseDir creates the base directory structure
func EnsureBaseDir(baseDir string, fsys afero.Fs) error {
	dirs := []string{
		filepath.Join(baseDir, "cluster"),
		filepath.Join(baseDir, "schemas"),
	}
	for _, dir := range dirs {
		if err := fsys.MkdirAll(dir, 0755); err != nil {
			return errors.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	return nil
}

// CleanOutputDir removes existing structured dump files
func CleanOutputDir(baseDir string, fsys afero.Fs) error {
	dirs := []string{
		filepath.Join(baseDir, "cluster"),
		filepath.Join(baseDir, "schemas"),
	}
	for _, dir := range dirs {
		if exists, _ := afero.DirExists(fsys, dir); exists {
			if err := fsys.RemoveAll(dir); err != nil {
				return errors.Errorf("failed to remove directory %s: %w", dir, err)
			}
		}
	}
	return nil
}

// ValidateOutputDir checks if the output directory is safe to write to
func ValidateOutputDir(baseDir string, fsys afero.Fs) error {
	// Check if migrations directory exists (indicates supabase project)
	migrationsDir := filepath.Join(baseDir, "migrations")
	if exists, _ := afero.DirExists(fsys, migrationsDir); !exists {
		// Check for config.toml
		configPath := filepath.Join(baseDir, "config.toml")
		if exists, _ := afero.Exists(fsys, configPath); !exists {
			return errors.Errorf("directory %s does not appear to be a Supabase project (no migrations/ or config.toml)", baseDir)
		}
	}
	return nil
}

// SanitizeFileName sanitizes a name for use as a filename
func SanitizeFileName(name string) string {
	// Replace characters that are problematic in filenames
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}

// Utility for testing - create a minimal filesystem structure
func CreateTestStructure(fsys afero.Fs, baseDir string) error {
	// Create migrations directory to make it look like a supabase project
	if err := fsys.MkdirAll(filepath.Join(baseDir, "migrations"), 0755); err != nil {
		return err
	}
	// Create an empty config.toml
	return afero.WriteFile(fsys, filepath.Join(baseDir, "config.toml"), []byte{}, 0644)
}

// GetOutputDir returns the output directory, creating it if it doesn't exist
func GetOutputDir(baseDir string, fsys afero.Fs) (string, error) {
	if baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", errors.Errorf("failed to get working directory: %w", err)
		}
		baseDir = filepath.Join(cwd, "supabase")
	}

	if err := fsys.MkdirAll(baseDir, 0755); err != nil {
		return "", errors.Errorf("failed to create output directory: %w", err)
	}

	return baseDir, nil
}
