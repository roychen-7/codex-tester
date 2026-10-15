package querymcp

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"gorm.io/gorm"
)

// This is a fixed metadata inventory, not the Agent's query allowlist.
var inspectionTables = []string{"users", "payment_orders", "usage_logs", "redeem_codes", "usage_dashboard_daily"}

type InspectedColumn struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Nullable  bool   `json:"nullable"`
	CanSelect bool   `json:"can_select"`
}

type InspectedTable struct {
	Name        string            `json:"name"`
	Exists      bool              `json:"exists"`
	SchemaUsage bool              `json:"schema_usage"`
	CanSelect   bool              `json:"can_select"`
	Columns     []InspectedColumn `json:"columns" gorm:"-"`
	Indexes     []string          `json:"indexes" gorm:"-"`
}

type Inspection struct {
	Version             string           `json:"version"`
	TransactionReadOnly string           `json:"transaction_read_only"`
	Tables              []InspectedTable `json:"tables" gorm:"-"`
}

// Inspect reads system catalogs only. It never reads business rows, runs ANALYZE,
// creates database objects or changes grants. Missing access is reported as data.
func (s *Service) Inspect(ctx context.Context) (Inspection, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out := Inspection{Tables: []InspectedTable{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for name, value := range map[string]string{"statement_timeout": "3000ms", "lock_timeout": "1000ms"} {
			if err := tx.Exec("SELECT set_config(?, ?, true)", name, value).Error; err != nil {
				return err
			}
		}

		if err := tx.Raw("SELECT current_setting('server_version') AS version, current_setting('transaction_read_only') AS transaction_read_only").Scan(&out).Error; err != nil {
			return err
		}

		for _, name := range inspectionTables {
			table := InspectedTable{Name: name, Columns: []InspectedColumn{}, Indexes: []string{}}
			if err := tx.Raw(`SELECT true AS exists,
    has_schema_privilege(n.oid, 'USAGE') AS schema_usage,
    has_table_privilege(c.oid, 'SELECT') AS can_select
    FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = ? AND c.relname = ? AND c.relkind IN ('r', 'p')`, "public", name).Scan(&table).Error; err != nil {
				return err
			}
			if table.Exists {
				if err := tx.Raw(`SELECT a.attname AS name, pg_catalog.format_type(a.atttypid, a.atttypmod) AS type,
     NOT a.attnotnull AS nullable, has_column_privilege(c.oid, a.attnum, 'SELECT') AS can_select
     FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
     JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
     WHERE n.nspname = ? AND c.relname = ? AND a.attnum > 0 AND NOT a.attisdropped
     ORDER BY a.attnum LIMIT 512`, "public", name).Scan(&table.Columns).Error; err != nil {
					return err
				}
				if err := tx.Table("pg_catalog.pg_indexes").Where("schemaname = ? AND tablename = ?", "public", name).
					Order("indexname").Limit(128).Pluck("indexdef", &table.Indexes).Error; err != nil {
					return err
				}
			}
			table.Name = name
			out.Tables = append(out.Tables, table)
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Inspection{}, errors.New("只读结构检查失败或超时；未读取业务行")
	}
	return out, nil
}
