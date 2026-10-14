package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/roychen-7/codex-tester/apps/api/internal/chat"
	"github.com/roychen-7/codex-tester/apps/api/internal/config"
	"github.com/roychen-7/codex-tester/apps/api/internal/database"
	"github.com/roychen-7/codex-tester/apps/api/internal/llm"
)

func run() error {
	if len(os.Args) < 2 {
		return errors.New("用法: manage migrate | check-llm | check-dialogue | check-agent | create-user <username> <display-name>（密码从标准输入读取）")
	}

	if os.Args[1] == "check-llm" {
		return checkLLM()
	}

	if os.Args[1] == "check-dialogue" {
		return checkDialogue()
	}

	if os.Args[1] == "check-agent" {
		return checkAgent()
	}

	cfg, err := config.LoadChat()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := database.Open(ctx, cfg.DSN)
	if err != nil {
		return err
	}

	pool, _ := db.DB()
	defer pool.Close()

	switch os.Args[1] {
	case "migrate":
		if err = database.Migrate(ctx, db); err != nil {
			return errors.New("数据库迁移失败，请检查迁移文件与数据库状态")
		}

		fmt.Println("Agent 数据库迁移完成")
	case "create-user":
		if len(os.Args) != 4 {
			return errors.New("用法: manage create-user <username> <display-name>，密码从标准输入读取")
		}

		password, e := bufio.NewReader(io.LimitReader(os.Stdin, 1024)).ReadString('\n')
		if e != nil && !errors.Is(e, io.EOF) {
			return e
		}

		password = strings.TrimSuffix(strings.TrimSuffix(password, "\n"), "\r")
		u, e := (&chat.Store{DB: db}).CreateUser(ctx, os.Args[2], os.Args[3], password)
		if e != nil {
			return e
		}

		fmt.Printf("已创建用户 %s（ID %d）\n", u.Username, u.ID)
	default:
		return errors.New("未知管理命令")
	}

	return nil
}

func checkLLM() error {
	cfg, err := config.LoadLLM()
	if err != nil {
		return err
	}

	if cfg.APIKey == "" {
		return errors.New("请先配置 OPENAI_API_KEY 和 OPENAI_MODEL")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	_, err = llm.NewOpenAI(cfg).Generate(ctx, []byte(`{"messages":[{"id":"1","role":"user","content":"我想分析客户留存，请先问我需要补充哪些信息。"}]}`))
	if err != nil {
		return err
	}

	fmt.Println("LLM 连接及结构化响应校验通过（仅使用测试问题，未读取业务数据）")
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
