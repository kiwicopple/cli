package migration

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ObjectFile represents a collection of SQL statements for a single object file
type ObjectFile struct {
	Category   string   // "cluster" or "schemas"
	Schema     string   // Schema name (empty for cluster-level)
	Type       string   // Directory type: "tables", "views", "functions", etc.
	Name       string   // Object name (file name without .sql)
	Statements []string // Ordered SQL statements
}

// FilePath returns the relative path for this object file
func (o *ObjectFile) FilePath() string {
	if o.Category == "cluster" {
		return fmt.Sprintf("cluster/%s.sql", o.Name)
	}
	if o.Type == "" {
		// Schema-level files like schema.sql, types.sql, sequences.sql
		return fmt.Sprintf("schemas/%s/%s.sql", o.Schema, o.Name)
	}
	return fmt.Sprintf("schemas/%s/%s/%s.sql", o.Schema, o.Type, o.Name)
}

// Content returns the file content with proper formatting
func (o *ObjectFile) Content() string {
	return strings.Join(o.Statements, "\n\n") + "\n"
}

// GroupStatements groups classified statements into object files
func GroupStatements(statements []ClassifiedStatement) map[string]*ObjectFile {
	objects := make(map[string]*ObjectFile)

	// First pass: collect all trigger functions and triggers
	triggerFuncs := make(map[string]*ClassifiedStatement) // schema.func_name -> statement
	triggers := make(map[string][]*ClassifiedStatement)   // schema.parent_name -> triggers

	for i := range statements {
		stmt := &statements[i]
		switch stmt.Type {
		case TypeTriggerFunction:
			key := qualifiedName(stmt.Schema, stmt.ObjectName)
			triggerFuncs[key] = stmt
		case TypeTrigger:
			key := qualifiedName(stmt.ParentSchema, stmt.ParentName)
			triggers[key] = append(triggers[key], stmt)
		}
	}

	// Build a map of trigger -> trigger function for association
	triggerToFunc := buildTriggerFunctionMap(triggers, triggerFuncs)

	// Track which trigger functions have been associated with a trigger
	usedTriggerFuncs := make(map[string]bool)
	for _, funcKey := range triggerToFunc {
		usedTriggerFuncs[funcKey] = true
	}

	// Second pass: group statements
	for i := range statements {
		stmt := &statements[i]

		// Skip trigger functions - they'll be added with their triggers
		if stmt.Type == TypeTriggerFunction {
			continue
		}

		// Skip triggers - they'll be added with their parent table/view
		if stmt.Type == TypeTrigger {
			continue
		}

		key, obj := classifyToObject(stmt)
		if obj == nil {
			continue
		}

		if existing, ok := objects[key]; ok {
			existing.Statements = append(existing.Statements, stmt.Statement)
		} else {
			obj.Statements = []string{stmt.Statement}
			objects[key] = obj
		}
	}

	// Third pass: add triggers and trigger functions to their parent objects
	for _, parentTriggers := range triggers {
		// Sort triggers by timing and name for consistent output
		sortTriggers(parentTriggers)

		for _, trigger := range parentTriggers {
			// Determine if parent is a table or view
			parentType := "tables"
			if trigger.TriggerTiming == TriggerInsteadOf {
				parentType = "views"
			}

			// Find or create the parent object file
			objKey := fmt.Sprintf("schemas/%s/%s/%s", trigger.ParentSchema, parentType, trigger.ParentName)
			parent := objects[objKey]
			if parent == nil {
				// Create placeholder if table/view wasn't in the dump
				parent = &ObjectFile{
					Category:   "schemas",
					Schema:     trigger.ParentSchema,
					Type:       parentType,
					Name:       trigger.ParentName,
					Statements: []string{},
				}
				objects[objKey] = parent
			}

			// Add trigger function first if exists
			triggerKey := qualifiedName(trigger.ParentSchema, trigger.ObjectName)
			if funcKey, ok := triggerToFunc[triggerKey]; ok {
				if triggerFunc, ok := triggerFuncs[funcKey]; ok {
					parent.Statements = append(parent.Statements, triggerFunc.Statement)
				}
			}

			// Add the trigger statement
			parent.Statements = append(parent.Statements, trigger.Statement)
		}
	}

	// Fourth pass: add orphan trigger functions to functions directory
	for funcKey, stmt := range triggerFuncs {
		if usedTriggerFuncs[funcKey] {
			continue
		}
		// Orphan trigger function - add to functions directory
		key := fmt.Sprintf("schemas/%s/functions/%s", stmt.Schema, stmt.ObjectName)
		if existing, ok := objects[key]; ok {
			existing.Statements = append(existing.Statements, stmt.Statement)
		} else {
			objects[key] = &ObjectFile{
				Category:   "schemas",
				Schema:     stmt.Schema,
				Type:       "functions",
				Name:       stmt.ObjectName,
				Statements: []string{stmt.Statement},
			}
		}
	}

	return objects
}

// classifyToObject converts a classified statement to an object file entry
func classifyToObject(stmt *ClassifiedStatement) (key string, obj *ObjectFile) {
	switch stmt.Type {
	// Cluster-level objects
	case TypeRole:
		return "cluster/roles", &ObjectFile{
			Category: "cluster",
			Name:     "roles",
		}
	case TypeExtension:
		return "cluster/extensions", &ObjectFile{
			Category: "cluster",
			Name:     "extensions",
		}
	case TypeForeignDataWrapper, TypeForeignServer, TypeUserMapping:
		return "cluster/foreign_data_wrappers", &ObjectFile{
			Category: "cluster",
			Name:     "foreign_data_wrappers",
		}
	case TypePublication:
		return "cluster/publications", &ObjectFile{
			Category: "cluster",
			Name:     "publications",
		}
	case TypeSubscription:
		return "cluster/subscriptions", &ObjectFile{
			Category: "cluster",
			Name:     "subscriptions",
		}
	case TypeEventTrigger:
		return "cluster/event_triggers", &ObjectFile{
			Category: "cluster",
			Name:     "event_triggers",
		}

	// Schema-level aggregate files
	case TypeSchema:
		key = fmt.Sprintf("schemas/%s/schema", stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.ObjectName,
			Name:     "schema",
		}
	case TypeType:
		key = fmt.Sprintf("schemas/%s/types", stmt.Schema)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Name:     "types",
		}
	case TypeSequence:
		key = fmt.Sprintf("schemas/%s/sequences", stmt.Schema)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Name:     "sequences",
		}

	// Schema-level individual files
	case TypeTable:
		key = fmt.Sprintf("schemas/%s/tables/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "tables",
			Name:     stmt.ObjectName,
		}
	case TypeForeignTable:
		key = fmt.Sprintf("schemas/%s/foreign_tables/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "foreign_tables",
			Name:     stmt.ObjectName,
		}
	case TypeView:
		key = fmt.Sprintf("schemas/%s/views/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "views",
			Name:     stmt.ObjectName,
		}
	case TypeMaterializedView:
		key = fmt.Sprintf("schemas/%s/materialized_views/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "materialized_views",
			Name:     stmt.ObjectName,
		}
	case TypeFunction:
		key = fmt.Sprintf("schemas/%s/functions/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "functions",
			Name:     stmt.ObjectName,
		}
	case TypeProcedure:
		key = fmt.Sprintf("schemas/%s/procedures/%s", stmt.Schema, stmt.ObjectName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Type:     "procedures",
			Name:     stmt.ObjectName,
		}

	// Parent-associated objects
	case TypeIndex, TypeConstraint:
		key = fmt.Sprintf("schemas/%s/tables/%s", stmt.ParentSchema, stmt.ParentName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.ParentSchema,
			Type:     "tables",
			Name:     stmt.ParentName,
		}
	case TypePolicy:
		key = fmt.Sprintf("schemas/%s/tables/%s", stmt.ParentSchema, stmt.ParentName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.ParentSchema,
			Type:     "tables",
			Name:     stmt.ParentName,
		}
	case TypeGrant:
		return classifyGrant(stmt)
	case TypeComment:
		return classifyComment(stmt)
	}

	return "", nil
}

// classifyGrant determines the target file for a GRANT statement
func classifyGrant(stmt *ClassifiedStatement) (key string, obj *ObjectFile) {
	if stmt.ParentName == "" {
		// Schema-level grant
		if stmt.ParentSchema != "" {
			key = fmt.Sprintf("schemas/%s/schema", stmt.ParentSchema)
			return key, &ObjectFile{
				Category: "schemas",
				Schema:   stmt.ParentSchema,
				Name:     "schema",
			}
		}
		// Cluster-level grant (role)
		return "cluster/roles", &ObjectFile{
			Category: "cluster",
			Name:     "roles",
		}
	}
	// Object-level grant - assume table unless we can determine otherwise
	key = fmt.Sprintf("schemas/%s/tables/%s", stmt.ParentSchema, stmt.ParentName)
	return key, &ObjectFile{
		Category: "schemas",
		Schema:   stmt.ParentSchema,
		Type:     "tables",
		Name:     stmt.ParentName,
	}
}

// classifyComment determines the target file for a COMMENT statement
func classifyComment(stmt *ClassifiedStatement) (key string, obj *ObjectFile) {
	if stmt.ParentName != "" {
		// Column comment - goes with parent table
		key = fmt.Sprintf("schemas/%s/tables/%s", stmt.ParentSchema, stmt.ParentName)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.ParentSchema,
			Type:     "tables",
			Name:     stmt.ParentName,
		}
	}
	// Object comment - could be table, view, function, etc.
	// Default to schema-level if we can't determine
	if stmt.Schema != "" {
		key = fmt.Sprintf("schemas/%s/schema", stmt.Schema)
		return key, &ObjectFile{
			Category: "schemas",
			Schema:   stmt.Schema,
			Name:     "schema",
		}
	}
	return "", nil
}

// buildTriggerFunctionMap creates a mapping from trigger key to trigger function key
func buildTriggerFunctionMap(triggers map[string][]*ClassifiedStatement, triggerFuncs map[string]*ClassifiedStatement) map[string]string {
	result := make(map[string]string)

	// Extract function name from trigger statements
	funcCallRegex := regexp.MustCompile(`EXECUTE\s+(?:FUNCTION|PROCEDURE)\s+(?:("?\w+"?\.)?("?\w+"?))\s*\(`)

	for _, triggerList := range triggers {
		for _, trigger := range triggerList {
			matches := funcCallRegex.FindStringSubmatch(trigger.Statement)
			if len(matches) >= 3 {
				funcSchema := strings.Trim(matches[1], `".`)
				if funcSchema == "" {
					funcSchema = trigger.ParentSchema
				}
				funcName := strings.Trim(matches[2], `"`)

				funcKey := qualifiedName(funcSchema, funcName)
				if _, ok := triggerFuncs[funcKey]; ok {
					triggerKey := qualifiedName(trigger.ParentSchema, trigger.ObjectName)
					result[triggerKey] = funcKey
				}
			}
		}
	}

	return result
}

func qualifiedName(schema, name string) string {
	if schema == "" {
		return name
	}
	return schema + "." + name
}

func sortTriggers(triggers []*ClassifiedStatement) {
	sort.Slice(triggers, func(i, j int) bool {
		// Sort by timing first (BEFORE < AFTER < INSTEAD OF)
		timingOrder := map[TriggerTiming]int{
			TriggerBefore:    0,
			TriggerAfter:     1,
			TriggerInsteadOf: 2,
		}
		if timingOrder[triggers[i].TriggerTiming] != timingOrder[triggers[j].TriggerTiming] {
			return timingOrder[triggers[i].TriggerTiming] < timingOrder[triggers[j].TriggerTiming]
		}
		// Then by name
		return triggers[i].ObjectName < triggers[j].ObjectName
	})
}

// SortedKeys returns sorted keys from the object map for deterministic output
func SortedKeys(objects map[string]*ObjectFile) []string {
	keys := make([]string, 0, len(objects))
	for k := range objects {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
