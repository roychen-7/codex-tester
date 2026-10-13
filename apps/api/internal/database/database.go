package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Open(ctx context.Context, dsn string) (*gorm.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName == "" {
		return nil, errors.New("无效的 Agent MySQL 连接配置")
	}

	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.MultiStatements = false
	cfg.InterpolateParams = false
	cfg.Timeout = 5 * time.Second
	cfg.ReadTimeout = 10 * time.Second
	cfg.WriteTimeout = 10 * time.Second
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}

	cfg.Params["time_zone"] = "'+00:00'"
	cfg.Collation = "utf8mb4_0900_ai_ci"

	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, errors.New("无效的 Agent MySQL 连接配置")
	}

	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(3 * time.Minute)
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, errors.New("无法连接 Agent MySQL，请检查数据库与配置")
	}

	orm, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: db}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC().Truncate(time.Microsecond)
		},
	})
	if err != nil {
		db.Close()
		return nil, errors.New("无法初始化 Agent 数据库")
	}

	return orm, nil
}

func Ready(ctx context.Context, db *gorm.DB) error {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return err
	}

	var applied []schemaMigration
	if err := db.WithContext(ctx).Find(&applied).Error; err != nil {
		return err
	}

	checksums := make(map[string]string, len(applied))
	for _, migration := range applied {
		checksums[migration.Version] = migration.Checksum
	}

	for _, entry := range entries {
		raw, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}

		if checksums[entry.Name()] != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			return errors.New("数据库迁移缺失或校验和不一致")
		}
	}

	return nil
}
