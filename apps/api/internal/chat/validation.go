package chat

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrInput        = errors.New("输入内容不符合要求")
	ErrAuth         = errors.New("用户名或密码错误")
	ErrSession      = errors.New("请重新登录")
	ErrNotFound     = errors.New("会话不存在")
	ErrConflict     = errors.New("请求标识已用于不同内容")
	ErrUserExists   = errors.New("用户名已存在")
	usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
	uuidPattern     = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

func ValidID(id string) bool {
	n, err := strconv.ParseUint(id, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == id
}

func Title(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > 200 {
		return "", ErrInput
	}

	return value, nil
}
