package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestClassifyStatement(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected ClassifiedStatement
	}{
		// Role statements
		{
			name: "create role",
			sql:  `CREATE ROLE "app_user" WITH NOLOGIN`,
			expected: ClassifiedStatement{
				Type:       TypeRole,
				ObjectName: "app_user",
			},
		},
		{
			name: "alter role",
			sql:  `ALTER ROLE "app_user" SET search_path TO public`,
			expected: ClassifiedStatement{
				Type:       TypeRole,
				ObjectName: "app_user",
			},
		},

		// Extension statements
		{
			name: "create extension",
			sql:  `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`,
			expected: ClassifiedStatement{
				Type:       TypeExtension,
				ObjectName: "uuid-ossp",
			},
		},

		// Schema statements
		{
			name: "create schema",
			sql:  `CREATE SCHEMA IF NOT EXISTS "api"`,
			expected: ClassifiedStatement{
				Type:       TypeSchema,
				ObjectName: "api",
			},
		},

		// Type statements
		{
			name: "create enum type",
			sql:  `CREATE TYPE public.status AS ENUM ('pending', 'active', 'inactive')`,
			expected: ClassifiedStatement{
				Type:       TypeType,
				Schema:     "public",
				ObjectName: "status",
			},
		},
		{
			name: "create domain",
			sql:  `CREATE DOMAIN public.email AS text CHECK (VALUE ~ '@')`,
			expected: ClassifiedStatement{
				Type:       TypeType,
				Schema:     "public",
				ObjectName: "email",
			},
		},

		// Sequence statements
		{
			name: "create sequence",
			sql:  `CREATE SEQUENCE public.users_id_seq AS integer`,
			expected: ClassifiedStatement{
				Type:       TypeSequence,
				Schema:     "public",
				ObjectName: "users_id_seq",
			},
		},

		// Table statements
		{
			name: "create table",
			sql:  `CREATE TABLE public.users (id integer PRIMARY KEY, name text)`,
			expected: ClassifiedStatement{
				Type:       TypeTable,
				Schema:     "public",
				ObjectName: "users",
			},
		},
		{
			name: "create table no schema",
			sql:  `CREATE TABLE employees (id integer PRIMARY KEY)`,
			expected: ClassifiedStatement{
				Type:       TypeTable,
				Schema:     "public", // Default
				ObjectName: "employees",
			},
		},

		// View statements
		{
			name: "create view",
			sql:  `CREATE VIEW public.active_users AS SELECT * FROM public.users WHERE active = true`,
			expected: ClassifiedStatement{
				Type:       TypeView,
				Schema:     "public",
				ObjectName: "active_users",
			},
		},

		// Materialized view statements
		{
			name: "create materialized view",
			sql:  `CREATE MATERIALIZED VIEW public.user_stats AS SELECT count(*) FROM public.users`,
			expected: ClassifiedStatement{
				Type:       TypeMaterializedView,
				Schema:     "public",
				ObjectName: "user_stats",
			},
		},

		// Function statements
		{
			name: "create function",
			sql:  `CREATE FUNCTION public.get_user(id integer) RETURNS text AS $$ SELECT name FROM users WHERE id = $1 $$ LANGUAGE sql`,
			expected: ClassifiedStatement{
				Type:       TypeFunction,
				Schema:     "public",
				ObjectName: "get_user",
			},
		},
		{
			name: "create trigger function",
			sql:  `CREATE FUNCTION public.update_timestamp() RETURNS TRIGGER AS $$ BEGIN NEW.updated_at = now(); RETURN NEW; END $$ LANGUAGE plpgsql`,
			expected: ClassifiedStatement{
				Type:       TypeTriggerFunction,
				Schema:     "public",
				ObjectName: "update_timestamp",
			},
		},

		// Procedure statements
		{
			name: "create procedure",
			sql:  `CREATE PROCEDURE public.process_order(order_id integer) LANGUAGE plpgsql AS $$ BEGIN UPDATE orders SET status = 'processed' WHERE id = order_id; END $$`,
			expected: ClassifiedStatement{
				Type:       TypeProcedure,
				Schema:     "public",
				ObjectName: "process_order",
			},
		},

		// Trigger statements
		{
			name: "create before trigger",
			sql:  `CREATE TRIGGER update_timestamp BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION update_timestamp()`,
			expected: ClassifiedStatement{
				Type:          TypeTrigger,
				ObjectName:    "update_timestamp",
				ParentSchema:  "public",
				ParentName:    "users",
				TriggerTiming: TriggerBefore,
			},
		},
		{
			name: "create after trigger",
			sql:  `CREATE TRIGGER log_changes AFTER INSERT ON public.users FOR EACH ROW EXECUTE FUNCTION log_insert()`,
			expected: ClassifiedStatement{
				Type:          TypeTrigger,
				ObjectName:    "log_changes",
				ParentSchema:  "public",
				ParentName:    "users",
				TriggerTiming: TriggerAfter,
			},
		},
		{
			name: "create instead of trigger",
			sql:  `CREATE TRIGGER handle_insert INSTEAD OF INSERT ON public.user_view FOR EACH ROW EXECUTE FUNCTION handle_view_insert()`,
			expected: ClassifiedStatement{
				Type:          TypeTrigger,
				ObjectName:    "handle_insert",
				ParentSchema:  "public",
				ParentName:    "user_view",
				TriggerTiming: TriggerInsteadOf,
			},
		},

		// Policy statements
		{
			name: "create policy",
			sql:  `CREATE POLICY users_select ON public.users FOR SELECT USING (auth.uid() = id)`,
			expected: ClassifiedStatement{
				Type:         TypePolicy,
				ObjectName:   "users_select",
				ParentSchema: "public",
				ParentName:   "users",
			},
		},

		// Index statements
		{
			name: "create index",
			sql:  `CREATE INDEX users_name_idx ON public.users (name)`,
			expected: ClassifiedStatement{
				Type:         TypeIndex,
				ObjectName:   "users_name_idx",
				ParentSchema: "public",
				ParentName:   "users",
			},
		},

		// Constraint statements (ALTER TABLE ADD CONSTRAINT)
		{
			name: "add constraint",
			sql:  `ALTER TABLE public.posts ADD CONSTRAINT posts_user_fkey FOREIGN KEY (user_id) REFERENCES public.users(id)`,
			expected: ClassifiedStatement{
				Type:         TypeConstraint,
				ObjectName:   "posts_user_fkey",
				ParentSchema: "public",
				ParentName:   "posts",
			},
		},

		// Grant statements
		{
			name: "grant on table",
			sql:  `GRANT SELECT ON public.users TO app_user`,
			expected: ClassifiedStatement{
				Type:         TypeGrant,
				ParentSchema: "public",
				ParentName:   "users",
			},
		},

		// Comment statements
		{
			name: "comment on column",
			sql:  `COMMENT ON COLUMN public.users.email IS 'User email address'`,
			expected: ClassifiedStatement{
				Type:         TypeComment,
				ObjectName:   "email",
				ParentSchema: "public",
				ParentName:   "users",
			},
		},

		// Foreign data wrapper statements
		{
			name: "create fdw",
			sql:  `CREATE FOREIGN DATA WRAPPER postgres_fdw`,
			expected: ClassifiedStatement{
				Type:       TypeForeignDataWrapper,
				ObjectName: "postgres_fdw",
			},
		},
		{
			name: "create foreign server",
			sql:  `CREATE SERVER external_db FOREIGN DATA WRAPPER postgres_fdw`,
			expected: ClassifiedStatement{
				Type:       TypeForeignServer,
				ObjectName: "external_db",
			},
		},

		// Publication/subscription statements
		{
			name: "create publication",
			sql:  `CREATE PUBLICATION my_pub FOR ALL TABLES`,
			expected: ClassifiedStatement{
				Type:       TypePublication,
				ObjectName: "my_pub",
			},
		},
		{
			name: "create subscription",
			sql:  `CREATE SUBSCRIPTION my_sub CONNECTION 'host=...' PUBLICATION my_pub`,
			expected: ClassifiedStatement{
				Type:       TypeSubscription,
				ObjectName: "my_sub",
			},
		},

		// Event trigger statements
		{
			name: "create event trigger",
			sql:  `CREATE EVENT TRIGGER ddl_trigger ON ddl_command_end EXECUTE FUNCTION log_ddl()`,
			expected: ClassifiedStatement{
				Type:       TypeEventTrigger,
				ObjectName: "ddl_trigger",
			},
		},

		// Foreign table statements
		{
			name: "create foreign table",
			sql:  `CREATE FOREIGN TABLE public.remote_users (id integer, name text) SERVER external_db`,
			expected: ClassifiedStatement{
				Type:       TypeForeignTable,
				Schema:     "public",
				ObjectName: "remote_users",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyStatement(tt.sql)

			assert.Equal(t, tt.expected.Type, result.Type, "Type mismatch")
			if tt.expected.ObjectName != "" {
				assert.Equal(t, tt.expected.ObjectName, result.ObjectName, "ObjectName mismatch")
			}
			if tt.expected.Schema != "" {
				assert.Equal(t, tt.expected.Schema, result.Schema, "Schema mismatch")
			}
			if tt.expected.ParentName != "" {
				assert.Equal(t, tt.expected.ParentName, result.ParentName, "ParentName mismatch")
			}
			if tt.expected.ParentSchema != "" {
				assert.Equal(t, tt.expected.ParentSchema, result.ParentSchema, "ParentSchema mismatch")
			}
			if tt.expected.TriggerTiming != "" {
				assert.Equal(t, tt.expected.TriggerTiming, result.TriggerTiming, "TriggerTiming mismatch")
			}
			// Always verify the statement is preserved
			assert.Equal(t, tt.sql, result.Statement, "Statement should be preserved")
		})
	}
}

func TestClassifyStatements(t *testing.T) {
	statements := []string{
		`CREATE TABLE public.users (id integer)`,
		`CREATE INDEX users_id_idx ON public.users (id)`,
		`CREATE VIEW public.active_users AS SELECT * FROM public.users`,
	}

	results := ClassifyStatements(statements)

	assert.Len(t, results, 3)
	assert.Equal(t, TypeTable, results[0].Type)
	assert.Equal(t, TypeIndex, results[1].Type)
	assert.Equal(t, TypeView, results[2].Type)
}

func TestClassifyUnknownStatement(t *testing.T) {
	// Invalid SQL should return TypeOther
	result := ClassifyStatement("NOT A VALID SQL STATEMENT")
	assert.Equal(t, TypeOther, result.Type)
	assert.Equal(t, "NOT A VALID SQL STATEMENT", result.Statement)
}

func TestIsSchemaScoped(t *testing.T) {
	assert.True(t, isSchemaScoped(TypeTable))
	assert.True(t, isSchemaScoped(TypeView))
	assert.True(t, isSchemaScoped(TypeFunction))
	assert.True(t, isSchemaScoped(TypeTriggerFunction))
	assert.False(t, isSchemaScoped(TypeRole))
	assert.False(t, isSchemaScoped(TypeExtension))
	assert.False(t, isSchemaScoped(TypeOther))
}
