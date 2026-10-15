package chat

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/agent"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"gorm.io/gorm"
)

func TestHistoryMySQL(t *testing.T) {
	dsn := config.TestDSN()
	if dsn == "" {
		t.Skip("TEST_MYSQL_DSN required")
	}
	ctx := context.Background()
	db, err := database.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}

	pool, _ := db.DB()
	defer pool.Close()
	if err := database.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	rollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
		}

		users := []Account{
			{User: User{Username: fmt.Sprintf("hist_a_%d", time.Now().UnixNano()), DisplayName: "A"}, PasswordHash: "unused", Status: "active"},
			{User: User{Username: fmt.Sprintf("hist_b_%d", time.Now().UnixNano()), DisplayName: "B"}, PasswordHash: "unused", Status: "active"},
		}

		for i := range users {
			must(tx.Create(&users[i]).Error)
		}

		convs := []Conversation{{UserID: users[0].ID, Title: "earlier"}, {UserID: users[0].ID, Title: "current"}, {UserID: users[1].ID, Title: "foreign"}}
		for i := range convs {
			must(tx.Create(&convs[i]).Error)
		}

		send := func(conv uint64, text string) Message {
			t.Helper()
			uuid, err := messageUUID()
			must(err)
			m := Message{ConversationID: conv, RequestID: uuid, Role: "user", Content: text}
			must(tx.Create(&m).Error)
			return m
		}

		first := send(convs[0].ID, "留存定义 100%_明确 "+strings.Repeat("中", 5000))
		for i := 0; i < 11; i++ {
			send(convs[0].ID, fmt.Sprintf("留存讨论 %d", i))
		}

		foreign := send(convs[2].ID, "留存秘密")
		trigger := send(convs[1].ID, "留存问题")
		future := send(convs[0].ID, "留存未来")
		h := runHistory{store: &Store{DB: tx}, conversation: convs[1].ID, cutoff: trigger.ID}
		matches, err := h.Search(ctx, agent.HistorySearch{Query: "留存"})
		must(err)
		if len(matches.Items) != 10 || matches.NextCursor == "" {
			t.Fatal("pagination missing", matches)
		}

		more, err := h.Search(ctx, agent.HistorySearch{Query: "留存", BeforeID: matches.NextCursor})
		must(err)
		if len(more.Items) != 2 || more.NextCursor != "" {
			t.Fatal("wrong page", more)
		}

		for _, m := range append(matches.Items, more.Items...) {
			if m.ConversationID != strconv.FormatUint(convs[0].ID, 10) {
				t.Fatal("foreign or future message leaked")
			}
		}

		literal, err := h.Search(ctx, agent.HistorySearch{Query: "%_"})
		must(err)
		if len(literal.Items) != 1 {
			t.Fatal("wildcards not escaped")
		}

		read, err := h.Read(ctx, strconv.FormatUint(first.ID, 10))
		must(err)
		if len(read) != 3 || !read[0].Truncated || len([]rune(read[0].Content)) != 4000 {
			t.Fatal("bounded original/context missing")
		}

		for _, id := range []string{strconv.FormatUint(foreign.ID, 10), strconv.FormatUint(future.ID, 10), strconv.FormatUint(trigger.ID, 10), "0", "1 OR 1=1"} {
			if _, err := h.Read(ctx, id); err == nil {
				t.Fatal("unauthorized read", id)
			}
		}

		for _, args := range []agent.HistorySearch{{Query: " "}, {Query: "留存", BeforeID: "1 OR 1=1"}} {
			if _, err := h.Search(ctx, args); err == nil {
				t.Fatal("invalid search accepted")
			}
		}

		// The recent-history window is fixed before filtering by keyword.
		batch := make([]Message, 2000)
		for i := range batch {
			uuid, err := messageUUID()
			must(err)
			batch[i] = Message{ConversationID: convs[0].ID, RequestID: uuid, Role: "user", Content: "较新的无关消息"}
		}
		must(tx.CreateInBatches(&batch, 100).Error)
		newest := send(convs[1].ID, "新的触发消息")
		h.cutoff = newest.ID
		bounded, err := h.Search(ctx, agent.HistorySearch{Query: "留存"})
		must(err)
		if len(bounded.Items) != 0 || bounded.Coverage == "" {
			t.Fatal("recent-history bound not enforced")
		}
		if _, err := h.Read(ctx, strconv.FormatUint(first.ID, 10)); err != nil {
			t.Fatal("known older source should remain readable", err)
		}

		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
}
