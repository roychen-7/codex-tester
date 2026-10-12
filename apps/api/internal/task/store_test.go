package task

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestTitleCompatibility(t *testing.T) {
	s := NewStore()
	for _, title := range []string{strings.Repeat("x", 120), strings.Repeat("😀", 60)} {
		if _, err := s.Create(title); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Create(strings.Repeat("😀", 61)); !errors.Is(err, ErrTitle) {
		t.Fatal("must count UTF-16 code units")
	}
	item, err := s.Create("\ufeff\u00a0 标题 \n")
	if err != nil || item.Title != "标题" {
		t.Fatalf("%+v %v", item, err)
	}
}

func TestOrderAndCopies(t *testing.T) {
	s := NewStore()
	a, _ := s.Create("first")
	b, _ := s.Create("second")
	_, _ = s.Update(a.ID, "done")
	list := s.List()
	if len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID {
		t.Fatal(list)
	}
	list[0].Title = "changed outside store"
	if s.List()[0].Title != "first" {
		t.Fatal("list must return copies")
	}
	if err := s.Delete(a.ID); err != nil {
		t.Fatal(err)
	}
	if list := s.List(); len(list) != 1 || list[0].ID != b.ID {
		t.Fatal(list)
	}
}

func TestConcurrentAccess(t *testing.T) {
	s := NewStore()
	var group sync.WaitGroup
	for range 30 {
		group.Add(1)
		go func() {
			defer group.Done()
			item, err := s.Create("parallel")
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := s.Update(item.ID, "done"); err != nil {
				t.Error(err)
			}
			_ = s.List()
			if err := s.Delete(item.ID); err != nil {
				t.Error(err)
			}
		}()
	}
	group.Wait()
	if len(s.List()) != 0 {
		t.Fatal("all tasks should be deleted")
	}
}
