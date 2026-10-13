package task

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"unicode/utf16"
)

var (
	ErrTitle    = errors.New("标题应为 1–120 字的非空字符串。")
	ErrStatus   = errors.New("状态应为 todo、doing 或 done。")
	ErrNotFound = errors.New("任务不存在。")
)

type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

// Store preserves insertion order and protects access from concurrent handlers.
// It is intentionally in-memory until the database migration is reviewed.
type Store struct {
	mu    sync.RWMutex
	tasks []Task
}

func NewStore() *Store {
	return &Store{tasks: make([]Task, 0)}
}

func (s *Store) List() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Task, len(s.tasks))
	copy(result, s.tasks)
	return result
}

func (s *Store) Create(value any) (Task, error) {
	title, ok := value.(string)
	if !ok {
		return Task{}, ErrTitle
	}

	// Match JavaScript trim and length, including BOM and astral characters.
	title = strings.TrimFunc(title, func(r rune) bool {
		return strings.ContainsRune("\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff", r)
	})
	if len(utf16.Encode([]rune(title))) == 0 || len(utf16.Encode([]rune(title))) > 120 {
		return Task{}, ErrTitle
	}

	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Task{}, err
	}

	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	t := Task{
		ID:     fmt.Sprintf("%x-%x-%x-%x-%x", id[:4], id[4:6], id[6:8], id[8:10], id[10:]),
		Title:  title,
		Status: "todo",
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tasks = append(s.tasks, t)
	return t, nil
}

func (s *Store) Update(id string, value any) (Task, error) {
	status, ok := value.(string)
	if !ok || (status != "todo" && status != "doing" && status != "done") {
		return Task{}, ErrStatus
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.tasks {
		if s.tasks[i].ID == id {
			s.tasks[i].Status = status
			return s.tasks[i], nil
		}
	}

	return Task{}, ErrNotFound
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, t := range s.tasks {
		if t.ID == id {
			s.tasks = append(s.tasks[:i], s.tasks[i+1:]...)
			return nil
		}
	}

	return ErrNotFound
}
