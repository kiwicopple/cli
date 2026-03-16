package migration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupStatements(t *testing.T) {
	statements := []ClassifiedStatement{
		// Cluster level
		{Type: TypeRole, ObjectName: "app_user", Statement: "CREATE ROLE app_user"},
		{Type: TypeRole, ObjectName: "admin", Statement: "CREATE ROLE admin"},
		{Type: TypeExtension, ObjectName: "uuid-ossp", Statement: "CREATE EXTENSION uuid-ossp"},

		// Schema level
		{Type: TypeSchema, ObjectName: "public", Statement: "CREATE SCHEMA public"},
		{Type: TypeTable, Schema: "public", ObjectName: "users", Statement: "CREATE TABLE public.users (id int)"},
		{Type: TypeIndex, Schema: "public", ObjectName: "users_id_idx", ParentSchema: "public", ParentName: "users", Statement: "CREATE INDEX users_id_idx ON public.users (id)"},
		{Type: TypePolicy, ObjectName: "users_select", ParentSchema: "public", ParentName: "users", Statement: "CREATE POLICY users_select ON public.users"},

		// View with trigger
		{Type: TypeView, Schema: "public", ObjectName: "active_users", Statement: "CREATE VIEW public.active_users AS SELECT * FROM users"},

		// Functions
		{Type: TypeFunction, Schema: "public", ObjectName: "get_user", Statement: "CREATE FUNCTION public.get_user() RETURNS int"},
	}

	objects := GroupStatements(statements)

	// Check cluster files
	roles := objects["cluster/roles"]
	require.NotNil(t, roles, "Should have roles file")
	assert.Equal(t, "cluster", roles.Category)
	assert.Equal(t, "roles", roles.Name)
	assert.Len(t, roles.Statements, 2) // Two role statements

	extensions := objects["cluster/extensions"]
	require.NotNil(t, extensions, "Should have extensions file")
	assert.Len(t, extensions.Statements, 1)

	// Check schema files
	schema := objects["schemas/public/schema"]
	require.NotNil(t, schema, "Should have schema file")
	assert.Equal(t, "schemas", schema.Category)
	assert.Equal(t, "public", schema.Schema)
	assert.Len(t, schema.Statements, 1)

	// Check table with grouped statements (table + index + policy)
	users := objects["schemas/public/tables/users"]
	require.NotNil(t, users, "Should have users table file")
	assert.Equal(t, "tables", users.Type)
	assert.Equal(t, "users", users.Name)
	assert.Len(t, users.Statements, 3) // table + index + policy

	// Check view
	activeUsers := objects["schemas/public/views/active_users"]
	require.NotNil(t, activeUsers, "Should have active_users view file")
	assert.Equal(t, "views", activeUsers.Type)

	// Check function
	getUser := objects["schemas/public/functions/get_user"]
	require.NotNil(t, getUser, "Should have get_user function file")
	assert.Equal(t, "functions", getUser.Type)
}

func TestGroupTriggersWithTables(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypeTable, Schema: "public", ObjectName: "users", Statement: "CREATE TABLE public.users (id int)"},
		{Type: TypeTriggerFunction, Schema: "public", ObjectName: "update_ts", Statement: "CREATE FUNCTION public.update_ts() RETURNS TRIGGER"},
		{Type: TypeTrigger, ObjectName: "users_update", ParentSchema: "public", ParentName: "users", TriggerTiming: TriggerBefore, Statement: "CREATE TRIGGER users_update BEFORE UPDATE ON public.users EXECUTE FUNCTION public.update_ts()"},
	}

	objects := GroupStatements(statements)

	users := objects["schemas/public/tables/users"]
	require.NotNil(t, users)
	// Should have: table + trigger function + trigger
	assert.Len(t, users.Statements, 3)
}

func TestGroupInsteadOfTriggersWithViews(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypeView, Schema: "public", ObjectName: "user_view", Statement: "CREATE VIEW public.user_view AS SELECT 1"},
		{Type: TypeTriggerFunction, Schema: "public", ObjectName: "handle_insert", Statement: "CREATE FUNCTION public.handle_insert() RETURNS TRIGGER"},
		{Type: TypeTrigger, ObjectName: "view_insert", ParentSchema: "public", ParentName: "user_view", TriggerTiming: TriggerInsteadOf, Statement: "CREATE TRIGGER view_insert INSTEAD OF INSERT ON public.user_view EXECUTE FUNCTION public.handle_insert()"},
	}

	objects := GroupStatements(statements)

	view := objects["schemas/public/views/user_view"]
	require.NotNil(t, view)
	// Should have: view + trigger function + trigger
	assert.Len(t, view.Statements, 3)
}

func TestObjectFilePath(t *testing.T) {
	tests := []struct {
		obj      ObjectFile
		expected string
	}{
		{
			obj:      ObjectFile{Category: "cluster", Name: "roles"},
			expected: "cluster/roles.sql",
		},
		{
			obj:      ObjectFile{Category: "cluster", Name: "extensions"},
			expected: "cluster/extensions.sql",
		},
		{
			obj:      ObjectFile{Category: "schemas", Schema: "public", Name: "schema"},
			expected: "schemas/public/schema.sql",
		},
		{
			obj:      ObjectFile{Category: "schemas", Schema: "public", Name: "types"},
			expected: "schemas/public/types.sql",
		},
		{
			obj:      ObjectFile{Category: "schemas", Schema: "public", Type: "tables", Name: "users"},
			expected: "schemas/public/tables/users.sql",
		},
		{
			obj:      ObjectFile{Category: "schemas", Schema: "api", Type: "functions", Name: "get_user"},
			expected: "schemas/api/functions/get_user.sql",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.obj.FilePath())
		})
	}
}

func TestObjectFileContent(t *testing.T) {
	obj := ObjectFile{
		Statements: []string{
			"CREATE TABLE users (id int)",
			"CREATE INDEX users_id_idx ON users (id)",
		},
	}

	expected := "CREATE TABLE users (id int)\n\nCREATE INDEX users_id_idx ON users (id)\n"
	assert.Equal(t, expected, obj.Content())
}

func TestSortedKeys(t *testing.T) {
	objects := map[string]*ObjectFile{
		"schemas/public/tables/users":    {},
		"cluster/roles":                  {},
		"schemas/api/functions/get_user": {},
		"cluster/extensions":             {},
	}

	keys := SortedKeys(objects)

	assert.Equal(t, []string{
		"cluster/extensions",
		"cluster/roles",
		"schemas/api/functions/get_user",
		"schemas/public/tables/users",
	}, keys)
}

func TestGroupForeignDataWrapperStatements(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypeForeignDataWrapper, ObjectName: "postgres_fdw", Statement: "CREATE FDW postgres_fdw"},
		{Type: TypeForeignServer, ObjectName: "remote", Statement: "CREATE SERVER remote"},
		{Type: TypeUserMapping, Statement: "CREATE USER MAPPING FOR postgres SERVER remote"},
	}

	objects := GroupStatements(statements)

	fdw := objects["cluster/foreign_data_wrappers"]
	require.NotNil(t, fdw)
	assert.Len(t, fdw.Statements, 3)
}

func TestGroupPublicationSubscriptionStatements(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypePublication, ObjectName: "my_pub", Statement: "CREATE PUBLICATION my_pub"},
		{Type: TypeSubscription, ObjectName: "my_sub", Statement: "CREATE SUBSCRIPTION my_sub"},
	}

	objects := GroupStatements(statements)

	pub := objects["cluster/publications"]
	require.NotNil(t, pub)
	assert.Len(t, pub.Statements, 1)

	sub := objects["cluster/subscriptions"]
	require.NotNil(t, sub)
	assert.Len(t, sub.Statements, 1)
}

func TestGroupEventTriggerStatements(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypeEventTrigger, ObjectName: "ddl_trigger", Statement: "CREATE EVENT TRIGGER ddl_trigger"},
	}

	objects := GroupStatements(statements)

	et := objects["cluster/event_triggers"]
	require.NotNil(t, et)
	assert.Len(t, et.Statements, 1)
}

func TestGroupMultipleSchemas(t *testing.T) {
	statements := []ClassifiedStatement{
		{Type: TypeSchema, ObjectName: "public", Statement: "CREATE SCHEMA public"},
		{Type: TypeSchema, ObjectName: "api", Statement: "CREATE SCHEMA api"},
		{Type: TypeTable, Schema: "public", ObjectName: "users", Statement: "CREATE TABLE public.users"},
		{Type: TypeTable, Schema: "api", ObjectName: "endpoints", Statement: "CREATE TABLE api.endpoints"},
	}

	objects := GroupStatements(statements)

	assert.NotNil(t, objects["schemas/public/schema"])
	assert.NotNil(t, objects["schemas/api/schema"])
	assert.NotNil(t, objects["schemas/public/tables/users"])
	assert.NotNil(t, objects["schemas/api/tables/endpoints"])
}
