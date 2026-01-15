package migration

import (
	"regexp"
	"strings"
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

// Regex patterns for SQL statement classification
var (
	// Identifier pattern: handles quoted and unquoted identifiers
	identPattern    = `(?:"([^"]+)"|([a-zA-Z_][a-zA-Z0-9_]*))`
	qualifiedIdent  = identPattern + `(?:\.` + identPattern + `)?`

	// Statement patterns
	createRoleRe       = regexp.MustCompile(`(?i)^\s*CREATE\s+ROLE\s+` + identPattern)
	alterRoleRe        = regexp.MustCompile(`(?i)^\s*ALTER\s+ROLE\s+` + identPattern)
	grantRoleRe        = regexp.MustCompile(`(?i)^\s*GRANT\s+` + identPattern + `\s+TO\s+`)

	createExtensionRe  = regexp.MustCompile(`(?i)^\s*CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?` + identPattern)

	createSchemaRe     = regexp.MustCompile(`(?i)^\s*CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?` + identPattern)

	createTypeRe       = regexp.MustCompile(`(?i)^\s*CREATE\s+TYPE\s+` + qualifiedIdent)
	createDomainRe     = regexp.MustCompile(`(?i)^\s*CREATE\s+DOMAIN\s+` + qualifiedIdent)

	createSequenceRe   = regexp.MustCompile(`(?i)^\s*CREATE\s+SEQUENCE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + qualifiedIdent)

	createTableRe      = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + qualifiedIdent)
	createForeignTableRe = regexp.MustCompile(`(?i)^\s*CREATE\s+FOREIGN\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?` + qualifiedIdent)

	createViewRe       = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMP(?:ORARY)?\s+)?VIEW\s+` + qualifiedIdent)
	createMatViewRe    = regexp.MustCompile(`(?i)^\s*CREATE\s+MATERIALIZED\s+VIEW\s+(?:IF\s+NOT\s+EXISTS\s+)?` + qualifiedIdent)

	createFunctionRe   = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+` + qualifiedIdent)
	createProcedureRe  = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?PROCEDURE\s+` + qualifiedIdent)
	returnsTriggerRe   = regexp.MustCompile(`(?i)RETURNS\s+TRIGGER\b`)

	createTriggerRe    = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:CONSTRAINT\s+)?TRIGGER\s+` + identPattern + `\s+(BEFORE|AFTER|INSTEAD\s+OF)\s+.*?\s+ON\s+` + qualifiedIdent)

	createPolicyRe     = regexp.MustCompile(`(?i)^\s*CREATE\s+POLICY\s+` + identPattern + `\s+ON\s+` + qualifiedIdent)

	createIndexRe      = regexp.MustCompile(`(?i)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?` + identPattern + `\s+ON\s+(?:ONLY\s+)?` + qualifiedIdent)

	alterTableRe       = regexp.MustCompile(`(?i)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?` + qualifiedIdent)
	addConstraintRe    = regexp.MustCompile(`(?i)ADD\s+CONSTRAINT\s+` + identPattern)

	grantOnRe          = regexp.MustCompile(`(?i)^\s*GRANT\s+.*?\s+ON\s+(?:ALL\s+\w+\s+IN\s+SCHEMA\s+` + identPattern + `|(?:TABLE\s+|SEQUENCE\s+|FUNCTION\s+|PROCEDURE\s+|SCHEMA\s+)?` + qualifiedIdent + `)`)

	commentOnRe        = regexp.MustCompile(`(?i)^\s*COMMENT\s+ON\s+(\w+)\s+` + qualifiedIdent)
	commentColumnRe    = regexp.MustCompile(`(?i)^\s*COMMENT\s+ON\s+COLUMN\s+` + qualifiedIdent + `\.` + identPattern)

	createFdwRe        = regexp.MustCompile(`(?i)^\s*CREATE\s+FOREIGN\s+DATA\s+WRAPPER\s+` + identPattern)
	createServerRe     = regexp.MustCompile(`(?i)^\s*CREATE\s+SERVER\s+` + identPattern)
	createUserMappingRe = regexp.MustCompile(`(?i)^\s*CREATE\s+USER\s+MAPPING\s+`)

	createPublicationRe  = regexp.MustCompile(`(?i)^\s*CREATE\s+PUBLICATION\s+` + identPattern)
	createSubscriptionRe = regexp.MustCompile(`(?i)^\s*CREATE\s+SUBSCRIPTION\s+` + identPattern)

	createEventTriggerRe = regexp.MustCompile(`(?i)^\s*CREATE\s+EVENT\s+TRIGGER\s+` + identPattern)
)

// ClassifyStatement classifies a single SQL statement
func ClassifyStatement(sql string) ClassifiedStatement {
	result := ClassifiedStatement{
		Type:      TypeOther,
		Statement: sql,
	}

	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)

	// Role statements
	if matches := createRoleRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeRole
		result.ObjectName = extractName(matches, 1)
		return result
	}
	if matches := alterRoleRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeRole
		result.ObjectName = extractName(matches, 1)
		return result
	}
	if grantRoleRe.MatchString(trimmed) && !strings.Contains(upper, " ON ") {
		result.Type = TypeRole
		return result
	}

	// Extension
	if matches := createExtensionRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeExtension
		result.ObjectName = extractName(matches, 1)
		return result
	}

	// Foreign Data Wrapper related
	if matches := createFdwRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeForeignDataWrapper
		result.ObjectName = extractName(matches, 1)
		return result
	}
	if matches := createServerRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeForeignServer
		result.ObjectName = extractName(matches, 1)
		return result
	}
	if createUserMappingRe.MatchString(trimmed) {
		result.Type = TypeUserMapping
		return result
	}

	// Publication/Subscription
	if matches := createPublicationRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypePublication
		result.ObjectName = extractName(matches, 1)
		return result
	}
	if matches := createSubscriptionRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeSubscription
		result.ObjectName = extractName(matches, 1)
		return result
	}

	// Event Trigger
	if matches := createEventTriggerRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeEventTrigger
		result.ObjectName = extractName(matches, 1)
		return result
	}

	// Schema
	if matches := createSchemaRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeSchema
		result.ObjectName = extractName(matches, 1)
		return result
	}

	// Type/Domain
	if matches := createTypeRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeType
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}
	if matches := createDomainRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeType
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Sequence
	if matches := createSequenceRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeSequence
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Foreign Table (must check before regular table)
	if matches := createForeignTableRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeForeignTable
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Table
	if matches := createTableRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeTable
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Materialized View (must check before regular view)
	if matches := createMatViewRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeMaterializedView
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// View
	if matches := createViewRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeView
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Procedure (must check before function)
	if matches := createProcedureRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeProcedure
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		return result
	}

	// Function
	if matches := createFunctionRe.FindStringSubmatch(trimmed); matches != nil {
		result.Schema, result.ObjectName = extractQualifiedName(matches, 1)
		if returnsTriggerRe.MatchString(trimmed) {
			result.Type = TypeTriggerFunction
		} else {
			result.Type = TypeFunction
		}
		return result
	}

	// Trigger
	if matches := createTriggerRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeTrigger
		result.ObjectName = extractName(matches, 1)

		// Extract timing
		timing := strings.ToUpper(matches[3])
		if strings.Contains(timing, "INSTEAD") {
			result.TriggerTiming = TriggerInsteadOf
		} else if timing == "BEFORE" {
			result.TriggerTiming = TriggerBefore
		} else {
			result.TriggerTiming = TriggerAfter
		}

		// Extract parent table/view
		result.ParentSchema, result.ParentName = extractQualifiedName(matches, 4)
		return result
	}

	// Policy
	if matches := createPolicyRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypePolicy
		result.ObjectName = extractName(matches, 1)
		result.ParentSchema, result.ParentName = extractQualifiedName(matches, 3)
		return result
	}

	// Index
	if matches := createIndexRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeIndex
		result.ObjectName = extractName(matches, 1)
		result.ParentSchema, result.ParentName = extractQualifiedName(matches, 3)
		return result
	}

	// ALTER TABLE (for constraints)
	if matches := alterTableRe.FindStringSubmatch(trimmed); matches != nil {
		parentSchema, parentName := extractQualifiedName(matches, 1)
		if constraintMatches := addConstraintRe.FindStringSubmatch(trimmed); constraintMatches != nil {
			result.Type = TypeConstraint
			result.ObjectName = extractName(constraintMatches, 1)
			result.ParentSchema = parentSchema
			result.ParentName = parentName
			return result
		}
		// Other ALTER TABLE statements go with the table
		result.Type = TypeTable
		result.Schema = parentSchema
		result.ObjectName = parentName
		return result
	}

	// Grant
	if matches := grantOnRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeGrant
		// Try to extract schema and object
		if matches[1] != "" || matches[2] != "" {
			// GRANT ... ON ALL ... IN SCHEMA or ON SCHEMA
			result.ParentSchema = extractName(matches, 1)
		} else {
			result.ParentSchema, result.ParentName = extractQualifiedName(matches, 3)
		}
		return result
	}

	// Comment
	if matches := commentColumnRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeComment
		result.ParentSchema, result.ParentName = extractQualifiedName(matches, 1)
		result.ObjectName = extractName(matches, 5)
		return result
	}
	if matches := commentOnRe.FindStringSubmatch(trimmed); matches != nil {
		result.Type = TypeComment
		result.Schema, result.ObjectName = extractQualifiedName(matches, 2)
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

// extractName extracts a name from regex matches (handles quoted/unquoted)
func extractName(matches []string, startIdx int) string {
	if startIdx >= len(matches) {
		return ""
	}
	// Quoted name
	if matches[startIdx] != "" {
		return matches[startIdx]
	}
	// Unquoted name
	if startIdx+1 < len(matches) && matches[startIdx+1] != "" {
		return matches[startIdx+1]
	}
	return ""
}

// extractQualifiedName extracts schema.name from regex matches
func extractQualifiedName(matches []string, startIdx int) (schema, name string) {
	if startIdx >= len(matches) {
		return "public", ""
	}

	// First identifier (could be schema or name)
	first := extractName(matches, startIdx)

	// Second identifier (if present, first was schema)
	if startIdx+2 < len(matches) {
		second := extractName(matches, startIdx+2)
		if second != "" {
			return first, second
		}
	}

	// Only one identifier - default to public schema
	return "public", first
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
