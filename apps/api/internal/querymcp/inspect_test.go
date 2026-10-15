package querymcp

import (
	"context"
	"testing"
)

func TestInspectCatalogWithoutBusinessAccess(t *testing.T) {
	s := fixture(t)
	report, err := s.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Version == "" || report.TransactionReadOnly != "on" || len(report.Tables) != 5 {
		t.Fatal("missing version, read-only transaction or inventory")
	}
	expected := []string{"users", "payment_orders", "usage_logs", "redeem_codes", "usage_dashboard_daily"}
	for i, table := range report.Tables {
		if table.Name != expected[i] {
			t.Fatal("table identity was lost during metadata scan")
		}
		if i < 3 {
			if !table.Exists || table.CanSelect || table.SchemaUsage || len(table.Columns) == 0 || len(table.Indexes) == 0 {
				t.Fatalf("metadata should be readable despite missing business access: %s", table.Name)
			}
			for _, column := range table.Columns {
				if column.CanSelect || column.Name == "" || column.Type == "" {
					t.Fatal("incorrect column metadata or permissions")
				}
			}
		} else if table.Exists || len(table.Columns) != 0 || len(table.Indexes) != 0 {
			t.Fatal("missing table incorrectly reported")
		}
		if table.Name == "syslog" {
			t.Fatal("excluded table inspected")
		}
	}
}

func TestInspectCancelled(t *testing.T) {
	s := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	report, err := s.Inspect(ctx)
	if err == nil || len(report.Tables) != 0 {
		t.Fatal("cancelled inspection returned partial results")
	}
}
