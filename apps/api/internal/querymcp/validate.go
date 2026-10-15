package querymcp

import (
	"encoding/json"
	"errors"
	"strings"

	pgquery "github.com/pganalyze/pg_query_go/v6"
)

var ErrPolicy = errors.New("查询不符合允许的 SQL 范围：仅允许分析视图上的单条 SELECT 和受支持的函数")

var (
	relations = map[string]bool{"users": true, "payment_orders": true, "usage_logs": true}
	functions = map[string]bool{"count": true, "sum": true, "avg": true, "min": true, "max": true, "round": true, "date_trunc": true, "timezone": true, "abs": true}
	types     = map[string]bool{"text": true, "date": true, "timestamp": true, "timestamptz": true, "numeric": true, "int4": true, "int8": true, "float8": true, "bool": true}
	operators = map[string]bool{"=": true, "<>": true, "<": true, ">": true, "<=": true, ">=": true, "+": true, "-": true, "*": true, "/": true}
	nodes     = map[string]bool{
		"SelectStmt": true, "ResTarget": true, "ColumnRef": true, "A_Star": true,
		"A_Const": true, "String": true, "Integer": true, "Float": true, "Boolean": true,
		"RangeVar": true, "Alias": true, "A_Expr": true, "BoolExpr": true,
		"NullTest": true, "BooleanTest": true, "FuncCall": true, "TypeCast": true,
		"TypeName": true, "ParamRef": true, "SortBy": true, "JoinExpr": true,
		"RangeSubselect": true, "SubLink": true, "CaseExpr": true, "CaseWhen": true,
		"CoalesceExpr": true, "List": true,
	}
)

// Validate rejects unsupported AST nodes rather than relying on SQL keywords.
// CTEs, user-defined functions/types/operators and locking are deliberately unsupported in v1.
func Validate(sql string, params []string) (string, error) {
	if len(sql) == 0 || len(sql) > 16000 || strings.Contains(sql, "?") || len(params) > 16 {
		return "", ErrPolicy
	}

	for _, value := range params {
		if len(value) > 2000 {
			return "", ErrPolicy
		}
	}

	raw, err := pgquery.ParseToJSON(sql)
	if err != nil {
		return "", ErrPolicy
	}

	var root struct {
		Statements []struct {
			Statement map[string]any `json:"stmt"`
		} `json:"stmts"`
	}

	if json.Unmarshal([]byte(raw), &root) != nil || len(root.Statements) != 1 || root.Statements[0].Statement["SelectStmt"] == nil {
		return "", ErrPolicy
	}

	seen := map[int]bool{}
	var walk func(any) error
	walk = func(value any) error {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		case map[string]any:
			for key, child := range v {
				if len(key) > 0 && key[0] >= 'A' && key[0] <= 'Z' && !nodes[key] {
					return ErrPolicy
				}

				obj, _ := child.(map[string]any)
				switch key {
				case "SelectStmt":
					for _, forbidden := range []string{"intoClause", "lockingClause", "withClause"} {
						if obj[forbidden] != nil {
							return ErrPolicy
						}
					}
				case "RangeVar":
					name, _ := obj["relname"].(string)
					if obj["schemaname"] != "analytics" || obj["catalogname"] != nil || !relations[name] {
						return ErrPolicy
					}
				case "FuncCall":
					if !allowedName(obj["funcname"], functions) || obj["over"] != nil || obj["func_variadic"] == true {
						return ErrPolicy
					}
				case "TypeName", "typeName":
					if !allowedName(obj["names"], types) || obj["typmods"] != nil || obj["arrayBounds"] != nil {
						return ErrPolicy
					}
				case "A_Expr":
					if !allowedName(obj["name"], operators) {
						return ErrPolicy
					}
				case "SortBy":
					if obj["useOp"] != nil {
						return ErrPolicy
					}
				case "ParamRef":
					n, _ := obj["number"].(float64)
					if n < 1 || int(n) > len(params) {
						return ErrPolicy
					}

					seen[int(n)] = true
				}

				if err := walk(child); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if err := walk(root.Statements[0].Statement); err != nil {
		return "", err
	}

	if len(seen) != len(params) {
		return "", ErrPolicy
	}

	tree, err := pgquery.Parse(sql)
	if err != nil {
		return "", ErrPolicy
	}

	canonical, err := pgquery.Deparse(tree)
	if err != nil {
		return "", ErrPolicy
	}

	return canonical, nil
}

func allowedName(value any, allowed map[string]bool) bool {
	parts, ok := value.([]any)
	if !ok || len(parts) == 0 || len(parts) > 2 {
		return false
	}

	names := make([]string, 0, len(parts))
	for _, part := range parts {
		node, ok := part.(map[string]any)
		if !ok {
			return false
		}

		item, ok := node["String"].(map[string]any)
		if !ok {
			return false
		}

		name, ok := item["sval"].(string)
		if !ok {
			return false
		}

		names = append(names, name)
	}

	return (len(names) == 1 || names[0] == "pg_catalog") && allowed[names[len(names)-1]]
}
