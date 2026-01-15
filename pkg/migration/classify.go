package migration

import (
	"strings"

	"github.com/multigres/multigres/go/parser"
	"github.com/multigres/multigres/go/parser/ast"
)

// StatementType represents the type of a SQL statement for classification
type StatementType string

const (
	// Cluster-level types
	TypeRole               StatementType = "role"
	TypeExtension          StatementType = "extension"
	TypeForeignDataWrapper StatementType = "foreign_data_wrapper"
	TypeForeignServer      StatementType = "foreign_server"
	TypeUserMapping        StatementType = "user_mapping"
	TypePublication        StatementType = "publication"
	TypeSubscription       StatementType = "subscription"
	TypeEventTrigger       StatementType = "event_trigger"

	// Schema-level types
	TypeSchema           StatementType = "schema"
	TypeType             StatementType = "type"
	TypeSequence         StatementType = "sequence"
	TypeTable            StatementType = "table"
	TypeForeignTable     StatementType = "foreign_table"
	TypeView             StatementType = "view"
	TypeMaterializedView StatementType = "materialized_view"
	TypeFunction         StatementType = "function"
	TypeTriggerFunction  StatementType = "trigger_function"
	TypeProcedure        StatementType = "procedure"
	TypeTrigger          StatementType = "trigger"
	TypePolicy           StatementType = "policy"
	TypeIndex            StatementType = "index"
	TypeConstraint       StatementType = "constraint"
	TypeGrant            StatementType = "grant"
	TypeComment          StatementType = "comment"
	TypeOther            StatementType = "other"
)

// TriggerTiming represents when a trigger fires
type TriggerTiming string

const (
	TriggerBefore    TriggerTiming = "before"
	TriggerAfter     TriggerTiming = "after"
	TriggerInsteadOf TriggerTiming = "instead_of"
)

// ClassifiedStatement contains classification information for a SQL statement
type ClassifiedStatement struct {
	Type          StatementType
	Schema        string
	ObjectName    string
	ParentName    string
	ParentSchema  string
	TriggerTiming TriggerTiming
	Statement     string
}

// ClassifyStatement classifies a single SQL statement using the multigres parser
func ClassifyStatement(sql string) ClassifiedStatement {
	result := ClassifiedStatement{
		Type:      TypeOther,
		Statement: sql,
	}

	stmts, err := parser.ParseSQL(sql)
	if err != nil || len(stmts) == 0 {
		return result
	}

	stmt := stmts[0]

	switch node := stmt.(type) {
	// Role statements
	case *ast.CreateRoleStmt:
		result.Type = TypeRole
		result.ObjectName = node.Role
		return result

	case *ast.AlterRoleStmt:
		result.Type = TypeRole
		result.ObjectName = node.Role
		return result

	// Extension
	case *ast.CreateExtensionStmt:
		result.Type = TypeExtension
		result.ObjectName = node.Extname
		return result

	// Schema
	case *ast.CreateSchemaStmt:
		result.Type = TypeSchema
		result.ObjectName = node.Schemaname
		return result

	// Sequence
	case *ast.CreateSeqStmt:
		result.Type = TypeSequence
		if node.Sequence != nil {
			result.Schema = defaultSchema(node.Sequence.Schemaname)
			result.ObjectName = node.Sequence.Relname
		}
		return result

	// Table
	case *ast.CreateStmt:
		result.Type = TypeTable
		if node.Relation != nil {
			result.Schema = defaultSchema(node.Relation.Schemaname)
			result.ObjectName = node.Relation.Relname
		}
		return result

	// Foreign Table
	case *ast.CreateForeignTableStmt:
		result.Type = TypeForeignTable
		if node.Base != nil && node.Base.Relation != nil {
			result.Schema = defaultSchema(node.Base.Relation.Schemaname)
			result.ObjectName = node.Base.Relation.Relname
		}
		return result

	// View
	case *ast.ViewStmt:
		result.Type = TypeView
		if node.View != nil {
			result.Schema = defaultSchema(node.View.Schemaname)
			result.ObjectName = node.View.Relname
		}
		return result

	// Function/Procedure
	case *ast.CreateFunctionStmt:
		if node.Funcname != nil && len(node.Funcname) > 0 {
			result.Schema, result.ObjectName = extractQualifiedNameFromList(node.Funcname)
		}
		// Check if it's a trigger function
		if isTriggerFunction(node) {
			result.Type = TypeTriggerFunction
		} else if node.IsProcedure {
			result.Type = TypeProcedure
		} else {
			result.Type = TypeFunction
		}
		return result

	// Trigger
	case *ast.CreateTrigStmt:
		result.Type = TypeTrigger
		result.ObjectName = node.Trigname
		if node.Relation != nil {
			result.ParentSchema = defaultSchema(node.Relation.Schemaname)
			result.ParentName = node.Relation.Relname
		}
		// Determine timing
		result.TriggerTiming = getTriggerTiming(node)
		return result

	// Policy
	case *ast.CreatePolicyStmt:
		result.Type = TypePolicy
		result.ObjectName = node.PolicyName
		if node.Table != nil {
			result.ParentSchema = defaultSchema(node.Table.Schemaname)
			result.ParentName = node.Table.Relname
		}
		return result

	// Index
	case *ast.IndexStmt:
		result.Type = TypeIndex
		result.ObjectName = node.Idxname
		if node.Relation != nil {
			result.ParentSchema = defaultSchema(node.Relation.Schemaname)
			result.ParentName = node.Relation.Relname
		}
		return result

	// Type statements
	case *ast.CreateEnumStmt:
		result.Type = TypeType
		result.Schema, result.ObjectName = extractQualifiedNameFromList(node.TypeName)
		return result

	case *ast.CreateRangeStmt:
		result.Type = TypeType
		result.Schema, result.ObjectName = extractQualifiedNameFromList(node.TypeName)
		return result

	case *ast.CompositeTypeStmt:
		result.Type = TypeType
		if node.Typevar != nil {
			result.Schema = defaultSchema(node.Typevar.Schemaname)
			result.ObjectName = node.Typevar.Relname
		}
		return result

	case *ast.CreateDomainStmt:
		result.Type = TypeType
		result.Schema, result.ObjectName = extractQualifiedNameFromList(node.Domainname)
		return result

	// ALTER TABLE (for constraints)
	case *ast.AlterTableStmt:
		if node.Relation != nil {
			parentSchema := defaultSchema(node.Relation.Schemaname)
			parentName := node.Relation.Relname

			// Check if it's adding a constraint
			for _, cmd := range node.Cmds {
				if alterCmd, ok := cmd.(*ast.AlterTableCmd); ok {
					if alterCmd.Subtype == ast.AT_AddConstraint && alterCmd.Def != nil {
						if constraint, ok := alterCmd.Def.(*ast.Constraint); ok {
							result.Type = TypeConstraint
							result.ObjectName = constraint.Conname
							result.ParentSchema = parentSchema
							result.ParentName = parentName
							return result
						}
					}
				}
			}
			// Other ALTER TABLE statements go with the table
			result.Type = TypeTable
			result.Schema = parentSchema
			result.ObjectName = parentName
		}
		return result

	// Grant statements
	case *ast.GrantStmt:
		result.Type = TypeGrant
		if len(node.Objects) > 0 {
			if rangeVar, ok := node.Objects[0].(*ast.RangeVar); ok {
				result.ParentSchema = defaultSchema(rangeVar.Schemaname)
				result.ParentName = rangeVar.Relname
			}
		}
		return result

	case *ast.GrantRoleStmt:
		result.Type = TypeRole
		return result

	// Comment
	case *ast.CommentStmt:
		result.Type = TypeComment
		if node.Object != nil {
			switch obj := node.Object.(type) {
			case *ast.RangeVar:
				result.Schema = defaultSchema(obj.Schemaname)
				result.ObjectName = obj.Relname
			case []ast.Node:
				result.Schema, result.ObjectName = extractQualifiedNameFromNodes(obj)
			}
		}
		return result

	// Materialized View
	case *ast.CreateTableAsStmt:
		if node.Relkind == ast.OBJECT_MATVIEW {
			result.Type = TypeMaterializedView
			if node.Into != nil && node.Into.Rel != nil {
				result.Schema = defaultSchema(node.Into.Rel.Schemaname)
				result.ObjectName = node.Into.Rel.Relname
			}
		}
		return result

	// Foreign Data Wrapper
	case *ast.CreateFdwStmt:
		result.Type = TypeForeignDataWrapper
		result.ObjectName = node.Fdwname
		return result

	case *ast.CreateForeignServerStmt:
		result.Type = TypeForeignServer
		result.ObjectName = node.Servername
		return result

	case *ast.CreateUserMappingStmt:
		result.Type = TypeUserMapping
		return result

	// Publication/Subscription
	case *ast.CreatePublicationStmt:
		result.Type = TypePublication
		result.ObjectName = node.Pubname
		return result

	case *ast.CreateSubscriptionStmt:
		result.Type = TypeSubscription
		result.ObjectName = node.Subname
		return result

	// Event Trigger
	case *ast.CreateEventTrigStmt:
		result.Type = TypeEventTrigger
		result.ObjectName = node.Trigname
		return result
	}

	return result
}

// ClassifyStatements classifies multiple SQL statements
func ClassifyStatements(statements []string) []ClassifiedStatement {
	results := make([]ClassifiedStatement, 0, len(statements))
	for _, stmt := range statements {
		if strings.TrimSpace(stmt) == "" {
			continue
		}
		results = append(results, ClassifyStatement(stmt))
	}
	return results
}

// Helper functions

func defaultSchema(schema string) string {
	if schema == "" {
		return "public"
	}
	return schema
}

func extractQualifiedNameFromList(names []string) (schema, name string) {
	if len(names) == 0 {
		return "public", ""
	}
	if len(names) == 1 {
		return "public", names[0]
	}
	return names[0], names[len(names)-1]
}

func extractQualifiedNameFromNodes(nodes []ast.Node) (schema, name string) {
	var parts []string
	for _, n := range nodes {
		if str, ok := n.(*ast.String); ok {
			parts = append(parts, str.Sval)
		}
	}
	return extractQualifiedNameFromList(parts)
}

func isTriggerFunction(node *ast.CreateFunctionStmt) bool {
	if node.ReturnType == nil {
		return false
	}
	// Check if return type is TRIGGER
	if len(node.ReturnType.Names) > 0 {
		for _, n := range node.ReturnType.Names {
			if str, ok := n.(*ast.String); ok {
				if strings.EqualFold(str.Sval, "trigger") {
					return true
				}
			}
		}
	}
	return false
}

func getTriggerTiming(node *ast.CreateTrigStmt) TriggerTiming {
	if node.Timing&ast.TRIGGER_TYPE_INSTEAD != 0 {
		return TriggerInsteadOf
	}
	if node.Timing&ast.TRIGGER_TYPE_BEFORE != 0 {
		return TriggerBefore
	}
	return TriggerAfter
}

// isSchemaScoped returns true if the statement type is schema-scoped
func isSchemaScoped(t StatementType) bool {
	switch t {
	case TypeSchema, TypeType, TypeSequence, TypeTable, TypeForeignTable,
		TypeView, TypeMaterializedView, TypeFunction, TypeTriggerFunction,
		TypeProcedure, TypeTrigger, TypePolicy, TypeIndex, TypeConstraint:
		return true
	default:
		return false
	}
}
