package storage

import (
	"errors"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStoreAt(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func saveTestMemory(t *testing.T, store *Store, title string, commands []string, tags []string) int64 {
	t.Helper()
	m := &Memory{Title: title, Source: SourceSave, Tags: tags}
	for _, c := range commands {
		m.Commands = append(m.Commands, Command{Command: c})
	}
	if err := store.SaveMemory(m); err != nil {
		t.Fatalf("SaveMemory(%q): %v", title, err)
	}
	return m.ID
}

func TestGetMemory(t *testing.T) {
	store := newTestStore(t)
	id := saveTestMemory(t, store, "fix docker dns",
		[]string{"cat /etc/resolv.conf", "sudo systemctl restart docker"},
		[]string{"docker", "networking"})

	m, err := store.GetMemory(id)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}

	if m.Title != "fix docker dns" {
		t.Errorf("Title = %q, want %q", m.Title, "fix docker dns")
	}
	if m.Source != SourceSave {
		t.Errorf("Source = %q, want %q", m.Source, SourceSave)
	}
	if m.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero, want populated timestamp")
	}

	if len(m.Commands) != 2 {
		t.Fatalf("len(Commands) = %d, want 2", len(m.Commands))
	}
	if m.Commands[0].Command != "cat /etc/resolv.conf" || m.Commands[0].Position != 0 {
		t.Errorf("Commands[0] = %+v, want position 0 with resolv.conf", m.Commands[0])
	}
	if m.Commands[1].Command != "sudo systemctl restart docker" || m.Commands[1].Position != 1 {
		t.Errorf("Commands[1] = %+v, want position 1 with restart", m.Commands[1])
	}

	if len(m.Tags) != 2 || m.Tags[0] != "docker" || m.Tags[1] != "networking" {
		t.Errorf("Tags = %v, want [docker networking]", m.Tags)
	}
}

func TestGetMemoryNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetMemory(999)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestGetMemoriesByIDsPreservesOrder(t *testing.T) {
	store := newTestStore(t)
	id1 := saveTestMemory(t, store, "first", nil, nil)
	id2 := saveTestMemory(t, store, "second", nil, nil)
	id3 := saveTestMemory(t, store, "third", nil, nil)

	// Simulate vector-search ranking: not insertion order.
	memories, err := store.GetMemoriesByIDs([]int64{id2, id3, id1})
	if err != nil {
		t.Fatalf("GetMemoriesByIDs: %v", err)
	}

	want := []string{"second", "third", "first"}
	if len(memories) != len(want) {
		t.Fatalf("len = %d, want %d", len(memories), len(want))
	}
	for i, w := range want {
		if memories[i].Title != w {
			t.Errorf("memories[%d].Title = %q, want %q", i, memories[i].Title, w)
		}
	}
}

func TestGetMemoriesByIDsSkipsUnknownAndDuplicates(t *testing.T) {
	store := newTestStore(t)
	id := saveTestMemory(t, store, "only one", nil, nil)

	memories, err := store.GetMemoriesByIDs([]int64{999, id, id, 1000})
	if err != nil {
		t.Fatalf("GetMemoriesByIDs: %v", err)
	}

	if len(memories) != 1 || memories[0].Title != "only one" {
		t.Errorf("memories = %+v, want single 'only one'", memories)
	}
}

func TestGetMemoriesByIDsEmpty(t *testing.T) {
	store := newTestStore(t)

	memories, err := store.GetMemoriesByIDs(nil)
	if err != nil {
		t.Fatalf("GetMemoriesByIDs(nil): %v", err)
	}
	if len(memories) != 0 {
		t.Errorf("len = %d, want 0", len(memories))
	}
}

func TestGetMemoriesByIDsNoCrossContamination(t *testing.T) {
	store := newTestStore(t)
	id1 := saveTestMemory(t, store, "with stuff", []string{"ls"}, []string{"shell"})
	id2 := saveTestMemory(t, store, "bare", nil, nil)

	memories, err := store.GetMemoriesByIDs([]int64{id1, id2})
	if err != nil {
		t.Fatalf("GetMemoriesByIDs: %v", err)
	}
	if len(memories) != 2 {
		t.Fatalf("len = %d, want 2", len(memories))
	}
	if len(memories[1].Commands) != 0 || len(memories[1].Tags) != 0 {
		t.Errorf("bare memory got commands=%v tags=%v, want none",
			memories[1].Commands, memories[1].Tags)
	}
}
