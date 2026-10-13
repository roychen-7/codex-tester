package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

//go:embed migrations/*.sql
var migrations embed.FS

type schemaMigration struct {
	Version   string `gorm:"primaryKey"`
	Checksum  string
	AppliedAt time.Time
}

// Migrate runs versioned DDL using one GORM connection for the advisory lock.
// MySQL DDL is not transactional; the initial migration is restartable.
func Migrate(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Connection(func(conn *gorm.DB) error {
		// Keep the bound SQL connection but isolate each GORM statement's state.
		conn = conn.Session(&gorm.Session{NewDB: true})
		var locked int
		if err := conn.Raw("SELECT GET_LOCK('agent_session_schema', 10)").Scan(&locked).Error; err != nil || locked != 1 {
			return errors.New("无法取得迁移锁")
		}

		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn.WithContext(releaseCtx).Exec("DO RELEASE_LOCK('agent_session_schema')")
		}()

		const createLedger = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(100) PRIMARY KEY,
			checksum CHAR(64) NOT NULL,
			applied_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
		) ENGINE=InnoDB`
		if err := conn.Exec(createLedger).Error; err != nil {
			return err
		}

		entries, err := migrations.ReadDir("migrations")
		if err != nil {
			return err
		}

		for _, entry := range entries {
			if err := applyMigration(conn, entry.Name()); err != nil {
				return err
			}
		}

		return nil
	})
}

func applyMigration(conn *gorm.DB, name string) error {
	raw, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}

	checksum := fmt.Sprintf("%x", sha256.Sum256(raw))
	var existing schemaMigration
	err = conn.Where("version = ?", name).Take(&existing).Error
	if err == nil {
		if existing.Checksum != checksum {
			return errors.New("已执行迁移的校验和不一致")
		}

		return nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	// These embedded files contain only simple CREATE TABLE statements, not procedures.
	for _, statement := range strings.Split(string(raw), ";\n") {
		if strings.TrimSpace(statement) == "" {
			continue
		}

		if err := conn.Exec(statement).Error; err != nil {
			return fmt.Errorf("迁移 %s 失败: %w", name, err)
		}
	}

	return conn.Create(&schemaMigration{
		Version:   name,
		Checksum:  checksum,
		AppliedAt: time.Now().UTC(),
	}).Error
}
