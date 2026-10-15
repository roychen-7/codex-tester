package querymcp

import (
	"encoding/json"
	"fmt"
	"strings"

	pgquery "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	SourceSynthetic = "synthetic"
	SourceLive      = "live"
)

// All identifiers here are code-owned. The model can only query projections of
// these columns; public base tables are never accepted in model SQL.
var liveColumns = map[string][]string{
	"users":          {"id", "email", "role", "status", "created_at", "deleted_at", "balance", "total_recharged", "signup_source", "last_login_at", "last_active_at"},
	"payment_orders": {"id", "user_id", "amount", "pay_amount", "refund_amount", "status", "order_type", "payment_type", "paid_at", "completed_at", "refund_at", "created_at"},
	"usage_logs":     {"id", "user_id", "model", "requested_model", "upstream_model", "input_tokens", "output_tokens", "cache_creation_tokens", "cache_read_tokens", "total_cost", "actual_cost", "billing_type", "duration_ms", "first_token_ms", "created_at"},
}

// compileLive replaces each validated logical relation, including nested ones,
// with a field-limited subquery. It never creates a database view or object.
func compileLive(input PrepareInput) (string, []string, error) {
	canonical, err := Validate(input.SQL, input.Parameters)
	if err != nil {
		return "", nil, err
	}
	raw, err := pgquery.ParseToJSON(canonical)
	if err != nil {
		return "", nil, ErrPolicy
	}
	var tree map[string]any
	if json.Unmarshal([]byte(raw), &tree) != nil {
		return "", nil, ErrPolicy
	}
	bounded := false
	var walk func(any) error
	walk = func(value any) error {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				if err := walk(child); err != nil {
					return err
				}
			}
		case map[string]any:
			if relation, ok := node["RangeVar"].(map[string]any); ok {
				name, _ := relation["relname"].(string)
				columns, ok := liveColumns[name]
				if !ok || relation["schemaname"] != "analytics" {
					return ErrPolicy
				}
				projection := "SELECT " + strings.Join(columns, ", ") + " FROM public." + name
				timeColumn := ""
				if name == "payment_orders" {
					timeColumn = "paid_at"
				}
				if name == "usage_logs" {
					timeColumn = "created_at"
				}
				if timeColumn != "" {
					bounded = true
					projection += fmt.Sprintf(" WHERE %s >= $%d::timestamptz AND %s < $%d::timestamptz", timeColumn, len(input.Parameters)+1, timeColumn, len(input.Parameters)+2)
				}
				wrapper, err := pgquery.ParseToJSON("SELECT * FROM (" + projection + ") AS " + name)
				if err != nil {
					return ErrPolicy
				}
				var replacement struct {
					Statements []struct {
						Statement struct {
							Select struct {
								From []map[string]any `json:"fromClause"`
							} `json:"SelectStmt"`
						} `json:"stmt"`
					} `json:"stmts"`
				}
				if json.Unmarshal([]byte(wrapper), &replacement) != nil {
					return ErrPolicy
				}
				sub := replacement.Statements[0].Statement.Select.From[0]["RangeSubselect"].(map[string]any)
				if alias := relation["alias"]; alias != nil {
					sub["alias"] = alias
				}
				delete(node, "RangeVar")
				node["RangeSubselect"] = sub
				return nil // Never traverse generated public relations.
			}
			if column, ok := node["ColumnRef"].(map[string]any); ok {
				fields, _ := column["fields"].([]any)
				if len(fields) > 2 {
					if len(fields) != 3 {
						return ErrPolicy
					}
					first, _ := fields[0].(map[string]any)
					text, _ := first["String"].(map[string]any)
					if text["sval"] != "analytics" {
						return ErrPolicy
					}
					column["fields"] = fields[1:]
				}
			}
			for _, child := range node {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(tree); err != nil {
		return "", nil, err
	}
	encoded, err := json.Marshal(tree)
	if err != nil {
		return "", nil, ErrPolicy
	}
	parsed := &pgquery.ParseResult{}
	if protojson.Unmarshal(encoded, parsed) != nil {
		return "", nil, ErrPolicy
	}
	sql, err := pgquery.Deparse(parsed)
	if err != nil {
		return "", nil, ErrPolicy
	}
	params := append([]string{}, input.Parameters...)
	if bounded {
		params = append(params, input.WindowStart, input.WindowEnd)
	}
	return sql, params, nil
}

func (s *Service) compile(input PrepareInput) (string, []string, error) {
	if s.source == SourceLive {
		return compileLive(input)
	}
	sql, err := Validate(input.SQL, input.Parameters)
	return sql, append([]string{}, input.Parameters...), err
}
