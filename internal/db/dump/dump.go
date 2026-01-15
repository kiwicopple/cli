package dump

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/go-errors/errors"
	"github.com/jackc/pgconn"
	"github.com/spf13/afero"
	"github.com/supabase/cli/internal/utils"
	"github.com/supabase/cli/pkg/migration"
	"github.com/supabase/cli/pkg/parser"
)

func Run(ctx context.Context, path string, config pgconn.Config, dataOnly, roleOnly, dryRun bool, fsys afero.Fs, opts ...migration.DumpOptionFunc) error {
	// Initialize output stream
	outStream := (io.Writer)(os.Stdout)
	exec := DockerExec
	if dryRun {
		fmt.Fprintln(os.Stderr, "DRY RUN: *only* printing the pg_dump script to console.")
		exec = noExec
	} else if len(path) > 0 {
		f, err := fsys.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return errors.Errorf("failed to open dump file: %w", err)
		}
		defer f.Close()
		outStream = f
	}
	db := "remote"
	if utils.IsLocalDatabase(config) {
		db = "local"
	}
	if dataOnly {
		fmt.Fprintf(os.Stderr, "Dumping data from %s database...\n", db)
		return migration.DumpData(ctx, config, outStream, exec, opts...)
	} else if roleOnly {
		fmt.Fprintf(os.Stderr, "Dumping roles from %s database...\n", db)
		return migration.DumpRole(ctx, config, outStream, exec, opts...)
	}
	fmt.Fprintf(os.Stderr, "Dumping schemas from %s database...\n", db)
	return migration.DumpSchema(ctx, config, outStream, exec, opts...)
}

func noExec(ctx context.Context, script string, env []string, w io.Writer) error {
	envMap := make(map[string]string, len(env))
	for _, e := range env {
		index := strings.IndexByte(e, '=')
		if index < 0 {
			continue
		}
		envMap[e[:index]] = e[index+1:]
	}
	expanded := os.Expand(script, func(key string) string {
		// Bash variable expansion is unsupported:
		// https://github.com/golang/go/issues/47187
		parts := strings.Split(key, ":")
		value := envMap[parts[0]]
		// Escape double quotes in env vars
		return strings.ReplaceAll(value, `"`, `\"`)
	})
	fmt.Fprintln(w, expanded)
	return nil
}

func DockerExec(ctx context.Context, script string, env []string, w io.Writer) error {
	return utils.DockerRunOnceWithConfig(
		ctx,
		container.Config{
			Image: utils.Config.Db.Image,
			Env:   env,
			Cmd:   []string{"bash", "-c", script, "--"},
		},
		container.HostConfig{
			NetworkMode: network.NetworkHost,
		},
		network.NetworkingConfig{},
		"",
		w,
		os.Stderr,
	)
}

// RunStructured performs a structured dump to a directory hierarchy
func RunStructured(ctx context.Context, outputDir string, schemas []string, config pgconn.Config, fsys afero.Fs) error {
	// Determine output directory
	baseDir := outputDir
	if baseDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return errors.Errorf("failed to get working directory: %w", err)
		}
		baseDir = filepath.Join(cwd, "supabase")
	}

	db := "remote"
	if utils.IsLocalDatabase(config) {
		db = "local"
	}
	fmt.Fprintf(os.Stderr, "Dumping structured schema from %s database...\n", db)

	// Get the raw dump
	var buf strings.Builder
	if err := migration.DumpSchema(ctx, config, &buf, DockerExec); err != nil {
		return err
	}

	// Parse and classify statements
	statements, err := parser.SplitAndTrim(strings.NewReader(buf.String()))
	if err != nil {
		return errors.Errorf("failed to split SQL statements: %w", err)
	}

	classified := migration.ClassifyStatements(statements)

	// Group statements
	objects := migration.GroupStatements(classified)

	// Filter by schemas if specified
	if len(schemas) > 0 {
		objects = migration.FilterBySchemas(objects, schemas)
	}

	// Create directory structure
	if err := migration.EnsureBaseDir(baseDir, fsys); err != nil {
		return err
	}

	// Clean existing structured files
	if err := migration.CleanOutputDir(baseDir, fsys); err != nil {
		return err
	}

	// Write structured dump
	dumpConfig := migration.StructuredDumpConfig{
		BaseDir:      baseDir,
		Schemas:      schemas,
		IncludeRoles: true,
	}
	if err := migration.WriteStructuredDump(ctx, dumpConfig, objects, fsys); err != nil {
		return err
	}

	// Print summary
	migration.PrintSummary(os.Stderr, objects)

	// Generate and print suggested config
	paths, err := migration.GenerateSchemaPathsConfig(baseDir, fsys)
	if err != nil {
		return err
	}
	migration.PrintSchemaPathsConfig(os.Stderr, paths)

	return nil
}
