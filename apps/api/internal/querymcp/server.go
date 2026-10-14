package querymcp

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type subjectKey struct{}

type (
	DescribeInput struct {
		Table string `json:"table" jsonschema:"users, payment_orders or usage_logs"`
	}

	Column struct {
		Name string `json:"name" gorm:"column:column_name"`
		Type string `json:"type" gorm:"column:data_type"`
	}
)

type Dataset struct {
	Table         string   `json:"table"`
	Columns       []Column `json:"columns"`
	Notes         string   `json:"notes"`
	TimeColumn    string   `json:"time_column"`
	Relationships []string `json:"relationships"`
	Indexes       []string `json:"indexes"`
}

type ExecuteInput struct {
	QueryID string `json:"query_id"`
}

func (s *Service) Describe(ctx context.Context, input DescribeInput) (Dataset, error) {
	if !relations[input.Table] {
		return Dataset{}, ErrPolicy
	}

	out := Dataset{
		Table:   "analytics." + input.Table,
		Columns: []Column{},
		Notes: "仅为 sub2api v0.2.7 的脱敏字段子集。" +
			"payment_orders 按 paid_at、usage_logs 按 created_at 使用服务强制的左闭右开时间窗口。" +
			"金额口径、退款与订单状态必须由业务确认；日志行不自动等同成功请求。",
	}
	err := s.db.WithContext(ctx).Table("information_schema.columns").
		Select("column_name, data_type").
		Where("table_schema = ? AND table_name = ?", "analytics", input.Table).
		Order("ordinal_position").Scan(&out.Columns).Error
	if err != nil || len(out.Columns) == 0 {
		return Dataset{}, ErrQuery
	}

	out.Relationships = []string{}
	out.Indexes = []string{}
	if input.Table == "payment_orders" {
		out.TimeColumn = "paid_at"
		out.Relationships = []string{"user_id -> analytics.users.id"}
	} else if input.Table == "usage_logs" {
		out.TimeColumn = "created_at"
		out.Relationships = []string{"user_id -> analytics.users.id"}
	}

	err = s.db.WithContext(ctx).Table("pg_catalog.pg_indexes").
		Where("schemaname = ? AND tablename = ?", "public", input.Table).
		Order("indexname").Pluck("indexdef", &out.Indexes).Error
	if err != nil {
		return Dataset{}, ErrQuery
	}

	return out, nil
}

func NewHandler(s *Service, token string) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "ddshub-query", Version: "0.1.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "describe_dataset",
		Description: "Describe an allowed analysis view and its query boundaries.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in DescribeInput) (*mcp.CallToolResult, Dataset, error) {
		out, err := s.Describe(ctx, in)
		return nil, out, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "prepare_query",
		Description: "Validate one SELECT and inspect EXPLAIN without executing the query; return an owner-bound query_id valid for two minutes.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PrepareInput) (*mcp.CallToolResult, Prepared, error) {
		owner, _ := ctx.Value(subjectKey{}).(string)
		out, err := s.Prepare(ctx, owner, in)
		return nil, out, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "execute_query",
		Description: "Execute a prepared query once in a read-only transaction, with timeout and bounded output. Truncated rows are not a complete population.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ExecuteInput) (*mcp.CallToolResult, Result, error) {
		owner, _ := ctx.Value(subjectKey{}).(string)
		out, err := s.Execute(ctx, owner, in.QueryID)
		return nil, out, err
	})

	transport := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	expected := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}

		actual := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		if len(token) < 32 || subtle.ConstantTimeCompare(expected[:], actual[:]) != 1 {
			writeError(w, 401, "unauthorized")
			return
		}
		// No browser access. Subject is trusted only because the calling backend is authenticated.
		owner := r.Header.Get("X-Agent-Subject")
		if r.Header.Get("Origin") != "" || owner == "" || len(owner) > 128 || strings.ContainsAny(owner, "\r\n") {
			writeError(w, 403, "invalid caller context")
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
		ctx := context.WithValue(r.Context(), subjectKey{}, owner)
		transport.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
