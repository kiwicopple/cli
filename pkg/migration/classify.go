package migration

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v5"
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

// ClassifiedStatement represents a classified SQL statement with metadata
type ClassifiedStatement struct {
	Type          StatementType
	Schema        string        // e.g., "public" (empty for cluster-level)
	ObjectName    string        // e.g., "users"
	ParentName    string        // e.g., table name for index/trigger/policy
	ParentSchema  string        // Schema of parent object
	TriggerTiming TriggerTiming // For triggers: before, after, instead_of
	Statement     string        // Full SQL statement
}

// ClassifyStatement parses and classifies a SQL statement using the PostgreSQL parser
func ClassifyStatement(sql string) ClassifiedStatement {
	result := ClassifiedStatement{
		Type:      TypeOther,
		Statement: sql,
	}

	tree, err := pg_query.Parse(sql)
	if err != nil {
		return result
	}

	if len(tree.Stmts) == 0 {
		return result
	}

	stmt := tree.Stmts[0].Stmt
	if stmt == nil {
		return result
	}

	switch node := stmt.Node.(type) {
	// Role statements
	case *pg_query.Node_CreateRoleStmt:
		result.Type = TypeRole
		result.ObjectName = node.CreateRoleStmt.Role

	case *pg_query.Node_AlterRoleStmt:
		result.Type = TypeRole
		result.ObjectName = node.AlterRoleStmt.Role

	case *pg_query.Node_GrantRoleStmt:
		result.Type = TypeRole

	// Extension
	case *pg_query.Node_CreateExtensionStmt:
		result.Type = TypeExtension
		result.ObjectName = node.CreateExtensionStmt.Extname

	// Foreign Data Wrapper
	case *pg_query.Node_CreateFdwStmt:
		result.Type = TypeForeignDataWrapper
		result.ObjectName = node.CreateFdwStmt.Fdwname

	case *pg_query.Node_CreateForeignServerStmt:
		result.Type = TypeForeignServer
		result.ObjectName = node.CreateForeignServerStmt.Servername

	case *pg_query.Node_CreateUserMappingStmt:
		result.Type = TypeUserMapping

	// Replication
	case *pg_query.Node_CreatePublicationStmt:
		result.Type = TypePublication
		result.ObjectName = node.CreatePublicationStmt.Pubname

	case *pg_query.Node_CreateSubscriptionStmt:
		result.Type = TypeSubscription
		result.ObjectName = node.CreateSubscriptionStmt.Subname

	// Event Trigger
	case *pg_query.Node_CreateEventTrigStmt:
		result.Type = TypeEventTrigger
		result.ObjectName = node.CreateEventTrigStmt.Trigname

	// Schema
	case *pg_query.Node_CreateSchemaStmt:
		result.Type = TypeSchema
		result.ObjectName = node.CreateSchemaStmt.Schemaname

	// Types
	case *pg_query.Node_CreateEnumStmt:
		result.Type = TypeType
		if len(node.CreateEnumStmt.TypeName) > 0 {
			result.Schema, result.ObjectName = extractTypeName(node.CreateEnumStmt.TypeName)
		}

	case *pg_query.Node_CreateRangeStmt:
		result.Type = TypeType
		if len(node.CreateRangeStmt.TypeName) > 0 {
			result.Schema, result.ObjectName = extractTypeName(node.CreateRangeStmt.TypeName)
		}

	case *pg_query.Node_CompositeTypeStmt:
		result.Type = TypeType
		if node.CompositeTypeStmt.Typevar != nil {
			result.Schema = node.CompositeTypeStmt.Typevar.Schemaname
			result.ObjectName = node.CompositeTypeStmt.Typevar.Relname
		}

	case *pg_query.Node_CreateDomainStmt:
		result.Type = TypeType
		if len(node.CreateDomainStmt.Domainname) > 0 {
			result.Schema, result.ObjectName = extractTypeName(node.CreateDomainStmt.Domainname)
		}

	// Sequence
	case *pg_query.Node_CreateSeqStmt:
		result.Type = TypeSequence
		if node.CreateSeqStmt.Sequence != nil {
			result.Schema = node.CreateSeqStmt.Sequence.Schemaname
			result.ObjectName = node.CreateSeqStmt.Sequence.Relname
		}

	// Table
	case *pg_query.Node_CreateStmt:
		result.Type = TypeTable
		if node.CreateStmt.Relation != nil {
			result.Schema = node.CreateStmt.Relation.Schemaname
			result.ObjectName = node.CreateStmt.Relation.Relname
		}

	// Foreign Table
	case *pg_query.Node_CreateForeignTableStmt:
		result.Type = TypeForeignTable
		if node.CreateForeignTableStmt.Base != nil && node.CreateForeignTableStmt.Base.Relation != nil {
			result.Schema = node.CreateForeignTableStmt.Base.Relation.Schemaname
			result.ObjectName = node.CreateForeignTableStmt.Base.Relation.Relname
		}

	// View
	case *pg_query.Node_ViewStmt:
		result.Type = TypeView
		if node.ViewStmt.View != nil {
			result.Schema = node.ViewStmt.View.Schemaname
			result.ObjectName = node.ViewStmt.View.Relname
		}

	// Materialized View
	case *pg_query.Node_CreateTableAsStmt:
		if node.CreateTableAsStmt.Objtype == pg_query.ObjectType_OBJECT_MATVIEW {
			result.Type = TypeMaterializedView
			if node.CreateTableAsStmt.Into != nil && node.CreateTableAsStmt.Into.Rel != nil {
				result.Schema = node.CreateTableAsStmt.Into.Rel.Schemaname
				result.ObjectName = node.CreateTableAsStmt.Into.Rel.Relname
			}
		}

	// Function
	case *pg_query.Node_CreateFunctionStmt:
		result.ObjectName = extractFunctionName(node.CreateFunctionStmt.Funcname)
		result.Schema = extractFunctionSchema(node.CreateFunctionStmt.Funcname)

		// Check if it's a trigger function
		if isTriggerFunction(node.CreateFunctionStmt) {
			result.Type = TypeTriggerFunction
		} else if node.CreateFunctionStmt.IsProcedure {
			result.Type = TypeProcedure
		} else {
			result.Type = TypeFunction
		}

	// Trigger
	case *pg_query.Node_CreateTrigStmt:
		result.Type = TypeTrigger
		result.ObjectName = node.CreateTrigStmt.Trigname
		if node.CreateTrigStmt.Relation != nil {
			result.ParentSchema = node.CreateTrigStmt.Relation.Schemaname
			result.ParentName = node.CreateTrigStmt.Relation.Relname
		}
		// Determine trigger timing
		timing := node.CreateTrigStmt.Timing
		if timing&0x02 != 0 { // TRIGGER_TYPE_BEFORE
			result.TriggerTiming = TriggerBefore
		} else if timing&0x04 != 0 { // TRIGGER_TYPE_AFTER
			result.TriggerTiming = TriggerAfter
		} else if timing&0x40 != 0 { // TRIGGER_TYPE_INSTEAD
			result.TriggerTiming = TriggerInsteadOf
		}

	// Policy
	case *pg_query.Node_CreatePolicyStmt:
		result.Type = TypePolicy
		result.ObjectName = node.CreatePolicyStmt.PolicyName
		if node.CreatePolicyStmt.Table != nil {
			result.ParentSchema = node.CreatePolicyStmt.Table.Schemaname
			result.ParentName = node.CreatePolicyStmt.Table.Relname
		}

	// Index
	case *pg_query.Node_IndexStmt:
		result.Type = TypeIndex
		result.ObjectName = node.IndexStmt.Idxname
		if node.IndexStmt.Relation != nil {
			result.ParentSchema = node.IndexStmt.Relation.Schemaname
			result.ParentName = node.IndexStmt.Relation.Relname
		}

	// Alter Table (for constraints)
	case *pg_query.Node_AlterTableStmt:
		if node.AlterTableStmt.Relation != nil {
			result.ParentSchema = node.AlterTableStmt.Relation.Schemaname
			result.ParentName = node.AlterTableStmt.Relation.Relname
		}
		// Check if this is adding a constraint
		for _, cmd := range node.AlterTableStmt.Cmds {
			if alterCmd := cmd.GetAlterTableCmd(); alterCmd != nil {
				if alterCmd.Subtype == pg_query.AlterTableType_AT_AddConstraint {
					result.Type = TypeConstraint
					if alterCmd.Def != nil {
						if constraint := alterCmd.Def.GetConstraint(); constraint != nil {
							result.ObjectName = constraint.Conname
						}
					}
					break
				}
			}
		}

	// Grant/Revoke
	case *pg_query.Node_GrantStmt:
		result.Type = TypeGrant
		// Extract object info from grant
		if len(node.GrantStmt.Objects) > 0 {
			for _, obj := range node.GrantStmt.Objects {
				if rangeVar := obj.GetRangeVar(); rangeVar != nil {
					result.ParentSchema = rangeVar.Schemaname
					result.ParentName = rangeVar.Relname
					break
				}
			}
		}

	// Comment
	case *pg_query.Node_CommentStmt:
		result.Type = TypeComment
		result.ObjectName = extractCommentObjectName(node.CommentStmt)
		result.ParentSchema, result.ParentName = extractCommentParent(node.CommentStmt)

	// Alter sequence owned by
	case *pg_query.Node_AlterSeqStmt:
		result.Type = TypeSequence
		if node.AlterSeqStmt.Sequence != nil {
			result.Schema = node.AlterSeqStmt.Sequence.Schemaname
			result.ObjectName = node.AlterSeqStmt.Sequence.Relname
		}
	}

	// Default schema to public if not specified and it's a schema-scoped object
	if result.Schema == "" && isSchemaScoped(result.Type) {
		result.Schema = "public"
	}
	if result.ParentSchema == "" && result.ParentName != "" {
		result.ParentSchema = "public"
	}

	return result
}

// ClassifyStatements classifies multiple SQL statements
func ClassifyStatements(statements []string) []ClassifiedStatement {
	result := make([]ClassifiedStatement, len(statements))
	for i, stmt := range statements {
		result[i] = ClassifyStatement(stmt)
	}
	return result
}

// Helper functions

func extractTypeName(names []*pg_query.Node) (schema, name string) {
	switch len(names) {
	case 1:
		if str := names[0].GetString_(); str != nil {
			name = str.Sval
		}
	case 2:
		if str := names[0].GetString_(); str != nil {
			schema = str.Sval
		}
		if str := names[1].GetString_(); str != nil {
			name = str.Sval
		}
	}
	return
}

func extractFunctionName(names []*pg_query.Node) string {
	if len(names) == 0 {
		return ""
	}
	lastNode := names[len(names)-1]
	if str := lastNode.GetString_(); str != nil {
		return str.Sval
	}
	return ""
}

func extractFunctionSchema(names []*pg_query.Node) string {
	if len(names) < 2 {
		return ""
	}
	if str := names[0].GetString_(); str != nil {
		return str.Sval
	}
	return ""
}

func isTriggerFunction(stmt *pg_query.CreateFunctionStmt) bool {
	if stmt.ReturnType == nil {
		return false
	}
	for _, name := range stmt.ReturnType.Names {
		if str := name.GetString_(); str != nil {
			if strings.ToLower(str.Sval) == "trigger" {
				return true
			}
		}
	}
	return false
}

func extractCommentObjectName(stmt *pg_query.CommentStmt) string {
	if stmt.Object == nil {
		return ""
	}
	// Handle different object types
	if list := stmt.Object.GetList(); list != nil && len(list.Items) > 0 {
		lastItem := list.Items[len(list.Items)-1]
		if str := lastItem.GetString_(); str != nil {
			return str.Sval
		}
	}
	return ""
}

func extractCommentParent(stmt *pg_query.CommentStmt) (schema, name string) {
	// For column comments, extract table info
	if stmt.Objtype == pg_query.ObjectType_OBJECT_COLUMN {
		if list := stmt.Object.GetList(); list != nil && len(list.Items) >= 2 {
			// Format is usually schema.table.column or table.column
			switch len(list.Items) {
			case 3:
				if str := list.Items[0].GetString_(); str != nil {
					schema = str.Sval
				}
				if str := list.Items[1].GetString_(); str != nil {
					name = str.Sval
				}
			case 2:
				if str := list.Items[0].GetString_(); str != nil {
					name = str.Sval
				}
			}
		}
	}
	return
}

func isSchemaScoped(t StatementType) bool {
	switch t {
	case TypeSchema, TypeType, TypeSequence, TypeTable, TypeForeignTable,
		TypeView, TypeMaterializedView, TypeFunction, TypeTriggerFunction,
		TypeProcedure, TypeTrigger, TypePolicy, TypeIndex:
		return true
	}
	return false
}
