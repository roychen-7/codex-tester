package querymcp

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	ErrQuery  = errors.New("查询失败或超时，请检查允许的字段、参数和时间范围")
	ErrTicket = errors.New("query_id 无效、已使用、已过期或不属于当前任务，请重新 prepare_query")
)

type PrepareInput struct {
	SQL         string   `json:"sql" jsonschema:"Single SELECT over explicitly qualified analytics views; parameters use $1, $2"`
	Parameters  []string `json:"parameters" jsonschema:"Bound scalar values, in placeholder order"`
	Purpose     string   `json:"purpose"`
	WindowStart string   `json:"window_start" jsonschema:"Inclusive RFC3339 timestamp"`
	WindowEnd   string   `json:"window_end" jsonschema:"Exclusive RFC3339 timestamp; interval at most 31 days"`
}

type Prepared struct {
	QueryID       string    `json:"query_id"`
	ExpiresAt     time.Time `json:"expires_at"`
	SQL           string    `json:"sql"`
	EstimatedCost float64   `json:"estimated_cost"`
	EstimatedRows int64     `json:"estimated_rows"`
}

type Result struct {
	ResultID  string    `json:"result_id"`
	Columns   []string  `json:"columns"`
	Types     []string  `json:"types"`
	Rows      [][]any   `json:"rows"`
	Truncated bool      `json:"truncated"`
	ElapsedMS int64     `json:"elapsed_ms"`
	QueriedAt time.Time `json:"queried_at"`
}

type ticket struct {
	input   PrepareInput
	owner   string
	expires time.Time
}

type Service struct {
	db      *gorm.DB
	mu      sync.Mutex
	tickets map[string]ticket
	gate    chan struct{}
	timeout time.Duration
}

func Open(ctx context.Context, dsn string) (*Service, error) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: dsn}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, errors.New("无法连接查询 PostgreSQL")
	}

	pool, err := db.DB()
	if err != nil {
		return nil, errors.New("无法初始化查询连接池")
	}

	pool.SetMaxOpenConns(1)
	pool.SetMaxIdleConns(1)
	pool.SetConnMaxLifetime(5 * time.Minute)
	if err := pool.PingContext(ctx); err != nil {
		pool.Close()
		return nil, errors.New("查询 PostgreSQL 不可用")
	}

	var super bool
	if err := db.WithContext(ctx).Raw("SELECT rolsuper OR rolbypassrls OR rolcreaterole FROM pg_roles WHERE rolname = current_user").Scan(&super).Error; err != nil || super {
		pool.Close()
		return nil, errors.New("查询服务禁止使用高权限数据库角色")
	}

	return &Service{db: db, tickets: map[string]ticket{}, gate: make(chan struct{}, 1), timeout: 10 * time.Second}, nil
}

func (s *Service) Close() error {
	pool, err := s.db.DB()
	if err != nil {
		return err
	}

	return pool.Close()
}

func (s *Service) transaction(ctx context.Context, input PrepareInput, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	select {
	case s.gate <- struct{}{}:
		defer func() {
			<-s.gate
		}()
	case <-ctx.Done():
		return ErrQuery
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Configuration values are parameters, never interpolated SQL.
		for name, value := range map[string]string{
			"statement_timeout": fmt.Sprintf("%dms", s.timeout.Milliseconds()), "lock_timeout": "1000ms",
			"search_path": "pg_catalog", "TimeZone": "UTC", "agent.window_start": input.WindowStart, "agent.window_end": input.WindowEnd,
		} {
			if err := tx.Exec("SELECT set_config(?, ?, true)", name, value).Error; err != nil {
				return err
			}
		}

		return fn(tx)
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ErrQuery
	}

	return nil
}

func (s *Service) Prepare(ctx context.Context, owner string, input PrepareInput) (Prepared, error) {
	var out Prepared
	start, e1 := time.Parse(time.RFC3339, input.WindowStart)
	end, e2 := time.Parse(time.RFC3339, input.WindowEnd)
	if owner == "" || len(owner) > 128 || e1 != nil || e2 != nil || !end.After(start) || end.Sub(start) > 31*24*time.Hour || len(input.Purpose) == 0 || len(input.Purpose) > 1000 {
		return out, errors.New("需要任务身份、查询目的和不超过 31 天的有效时间范围")
	}

	canonical, err := Validate(input.SQL, input.Parameters)
	if err != nil {
		return out, err
	}

	input.SQL = canonical
	input.Parameters = append([]string(nil), input.Parameters...)
	var plan string
	err = s.transaction(ctx, input, func(tx *gorm.DB) error {
		return tx.Raw("EXPLAIN (FORMAT JSON) "+input.SQL, arguments(input.Parameters)...).Scan(&plan).Error
	})
	if err != nil {
		return out, err
	}

	var plans []struct {
		Plan struct {
			Cost float64 `json:"Total Cost"`
			Rows int64   `json:"Plan Rows"`
		} `json:"Plan"`
	}

	if json.Unmarshal([]byte(plan), &plans) != nil || len(plans) != 1 {
		return out, ErrQuery
	}

	if plans[0].Plan.Cost > 100000 || plans[0].Plan.Rows > 100000 {
		return out, errors.New("执行计划超出查询预算，请缩小范围或改用聚合")
	}

	id, err := randomID()
	if err != nil {
		return out, ErrQuery
	}

	out = Prepared{QueryID: id, ExpiresAt: time.Now().UTC().Add(2 * time.Minute), SQL: canonical, EstimatedCost: plans[0].Plan.Cost, EstimatedRows: plans[0].Plan.Rows}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, t := range s.tickets {
		if time.Now().After(t.expires) {
			delete(s.tickets, key)
		}
	}

	if len(s.tickets) >= 256 {
		return Prepared{}, errors.New("待执行查询已满，请稍后重试")
	}

	s.tickets[id] = ticket{input: input, owner: owner, expires: out.ExpiresAt}
	return out, nil
}

func (s *Service) Execute(ctx context.Context, owner, id string) (Result, error) {
	s.mu.Lock()
	t, ok := s.tickets[id]
	if ok && t.owner == owner {
		delete(s.tickets, id)
	}

	s.mu.Unlock()
	if !ok || t.owner != owner || time.Now().After(t.expires) {
		return Result{}, ErrTicket
	}

	if _, err := Validate(t.input.SQL, t.input.Parameters); err != nil {
		return Result{}, err
	}

	out := Result{ResultID: id, Rows: [][]any{}, QueriedAt: time.Now().UTC()}
	err := s.transaction(ctx, t.input, func(tx *gorm.DB) error {
		// The inner SQL is parser-validated and canonicalized; values remain bound.
		rows, err := tx.Raw("SELECT * FROM ("+t.input.SQL+") AS bounded_query LIMIT 201", arguments(t.input.Parameters)...).Rows()
		if err != nil {
			return err
		}

		defer rows.Close()
		out.Columns, err = rows.Columns()
		if err != nil {
			return err
		}

		columns, err := rows.ColumnTypes()
		if err != nil {
			return err
		}

		for _, column := range columns {
			out.Types = append(out.Types, column.DatabaseTypeName())
		}

		size := 0
		for rows.Next() {
			if len(out.Rows) == 200 {
				out.Truncated = true
				break
			}

			values := make([]any, len(columns))
			ptrs := make([]any, len(columns))
			for i := range values {
				ptrs[i] = &values[i]
			}

			if err := rows.Scan(ptrs...); err != nil {
				return err
			}

			for i, v := range values {
				switch n := v.(type) {
				case []byte:
					values[i] = string(n)
				case int64:
					values[i] = fmt.Sprint(n)
				case time.Time:
					values[i] = n.UTC().Format(time.RFC3339Nano)
				}
			}

			raw, err := json.Marshal(values)
			if err != nil {
				return err
			}

			size += len(raw)
			if size > 256*1024 {
				out.Truncated = true
				break
			}

			out.Rows = append(out.Rows, values)
		}

		return rows.Err()
	})
	if err != nil {
		return Result{}, err
	}

	out.ElapsedMS = time.Since(out.QueriedAt).Milliseconds()
	return out, nil
}

func arguments(values []string) []any {
	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}

	return args
}

func randomID() (string, error) {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf[:]), nil
}
