package database

import (
	"context"
	"database/sql"
	"errors"
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
	var row struct{ Version string }
	return db.WithContext(ctx).
		Table("schema_migrations").
		Where("version = ?", "001_sessions.sql").
		Take(&row).Error
}
