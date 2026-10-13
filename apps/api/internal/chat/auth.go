package chat

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("constant-dummy-for-missing-user"), bcrypt.DefaultCost)

func (s *Store) CreateUser(ctx context.Context, username, display, password string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	display = strings.TrimSpace(display)
	if !usernamePattern.MatchString(username) ||
		display == "" ||
		utf8.RuneCountInString(display) > 100 ||
		len(password) < 12 ||
		len(password) > 72 {
		return User{}, ErrInput
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	a := Account{
		User: User{
			Username:    username,
			DisplayName: display,
		},
		PasswordHash: string(hash),
		Status:       "active",
	}
	err = s.DB.WithContext(ctx).Create(&a).Error
	if err != nil {
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return User{}, ErrUserExists
		}

		return User{}, err
	}

	return a.User, nil
}

func (s *Store) Login(ctx context.Context, username, password string, ttl time.Duration) (User, string, error) {
	var a Account
	err := s.DB.WithContext(ctx).
		Where("username = ?", strings.ToLower(strings.TrimSpace(username))).
		Take(&a).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, "", err
	}

	hash := a.PasswordHash
	if errors.Is(err, gorm.ErrRecordNotFound) {
		hash = string(dummyHash)
	}

	match := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if err != nil || !match || a.Status != "active" {
		return User{}, "", ErrAuth
	}

	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return User{}, "", err
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	digest := sha256.Sum256([]byte(token))
	session := Session{
		UserID:    a.ID,
		TokenHash: digest[:],
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	err = s.DB.WithContext(ctx).Create(&session).Error
	return a.User, token, err
}

func (s *Store) Authenticate(ctx context.Context, token string) (User, error) {
	if len(token) != 43 {
		return User{}, ErrSession
	}

	digest := sha256.Sum256([]byte(token))
	var session Session
	err := s.DB.WithContext(ctx).
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", digest[:], time.Now().UTC()).
		Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return User{}, ErrSession
	}

	if err != nil {
		return User{}, err
	}

	var a Account
	err = s.DB.WithContext(ctx).Where("id = ? AND status = ?", session.UserID, "active").Take(&a).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrSession
	}

	return a.User, err
}

func (s *Store) Logout(ctx context.Context, token string) error {
	digest := sha256.Sum256([]byte(token))
	return s.DB.WithContext(ctx).
		Model(&Session{}).
		Where("token_hash = ? AND revoked_at IS NULL", digest[:]).
		Update("revoked_at", time.Now().UTC()).Error
}
