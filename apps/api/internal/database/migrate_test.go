package database

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"gorm.io/gorm"
)

func TestMigrationRecoveryMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("设置 TEST_MYSQL_DSN 启用迁移恢复测试")
	}

	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasSuffix(cfg.DBName, "_test") {
		t.Fatal("仅允许使用 _test 结尾的独立数据库")
	}

	ctx := context.Background()
	db, err := Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	defer pool.Close()
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// All ledger changes are uncommitted and rolled back. No DDL runs here:
	// the ALTER statements already committed and their markers must be detected.
	rollback := errors.New("rollback recovery test")
	err = db.Transaction(func(tx *gorm.DB) error {
		for _, version := range []string{"002_message_scope.sql", "003_conversation_context.sql"} {
			if err := tx.Where("version = ?", version).Delete(&schemaMigration{}).Error; err != nil {
				return err
			}

			if err := Ready(ctx, tx); err == nil {
				t.Fatal("missing ledger entry accepted by readiness check")
			}

			if err := applyMigration(tx, version); err != nil {
				t.Fatalf("recover %s after DDL commit: %v", version, err)
			}

			if err := Ready(ctx, tx); err != nil {
				t.Fatal(err)
			}
		}

		if err := tx.Model(&schemaMigration{}).
			Where("version = ?", "001_sessions.sql").
			Update("checksum", strings.Repeat("0", 64)).Error; err != nil {
			return err
		}

		if err := applyMigration(tx, "001_sessions.sql"); err == nil {
			t.Fatal("migration checksum mismatch accepted")
		}

		if err := Ready(ctx, tx); err == nil {
			t.Fatal("readiness ignored checksum mismatch")
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}

	if err := Ready(ctx, db); err != nil {
		t.Fatal("test did not restore migration ledger", err)
	}
}
