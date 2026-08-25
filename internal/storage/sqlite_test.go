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

func TestEmbeddingConfig(t *testing.T) {
	store := newTestStore(t)

	// Create new config
	ec1, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 128)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	if ec1.ID == 0 {
		t.Error("expected non-zero ID for new config")
	}
	if ec1.ModelName != "test-model" {
		t.Errorf("expected test-model, got %s", ec1.ModelName)
	}
	if ec1.Dimensions != 128 {
		t.Errorf("expected 128 dimensions, got %d", ec1.Dimensions)
	}

	// Fetch same config
	ec2, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 128)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	if ec1.ID != ec2.ID {
		t.Errorf("expected same ID %d, got %d", ec1.ID, ec2.ID)
	}
}

func TestEmbeddingStatus(t *testing.T) {
	store := newTestStore(t)
	memID := saveTestMemory(t, store, "status test", []string{"cmd1"}, nil)

	// Fetch memory to get command ID
	m, _ := store.GetMemory(memID)
	cmdID := m.Commands[0].ID

	// Create actual config to satisfy foreign key
	ec, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 10)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	configID := ec.ID

	status := &EmbeddingStatus{
		MemoryID:          memID,
		CommandID:         &cmdID,
		EmbeddingConfigID: configID,
		ChunkIndex:        0,
		ContentHash:       "hash1",
	}

	// Upsert new status
	err = store.UpsertEmbeddingStatus(status)
	if err != nil {
		t.Fatalf("UpsertEmbeddingStatus: %v", err)
	}
	if status.ID == 0 {
		t.Error("expected non-zero ID for new status")
	}

	// Fetch status
	st, err := store.GetEmbeddingStatus(memID, &cmdID, 0, configID)
	if err != nil {
		t.Fatalf("GetEmbeddingStatus: %v", err)
	}
	if st == nil {
		t.Fatal("expected status, got nil")
	}
	if st.ContentHash != "hash1" {
		t.Errorf("expected hash1, got %s", st.ContentHash)
	}

	// Upsert with new hash (update existing)
	status.ContentHash = "hash2"
	err = store.UpsertEmbeddingStatus(status)
	if err != nil {
		t.Fatalf("UpsertEmbeddingStatus (update): %v", err)
	}

	st, _ = store.GetEmbeddingStatus(memID, &cmdID, 0, configID)
	if st.ContentHash != "hash2" {
		t.Errorf("expected hash2, got %s", st.ContentHash)
	}
}

func TestDeleteMemory(t *testing.T) {
	store := newTestStore(t)
	memID := saveTestMemory(t, store, "to delete", []string{"rm -rf /"}, []string{"dangerous"})

	err := store.DeleteMemory(memID)
	if err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}

	_, err = store.GetMemory(memID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after deletion, got %v", err)
	}

	// Deleting again should return ErrNotFound
	err = store.DeleteMemory(memID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound when deleting non-existent ID, got %v", err)
	}
}

func TestListMemories(t *testing.T) {
	store := newTestStore(t)
	id1 := saveTestMemory(t, store, "mem 1", []string{"cmd1"}, []string{"tagA"})
	id2 := saveTestMemory(t, store, "mem 2", []string{"cmd2"}, []string{"tagB"})
	id3 := saveTestMemory(t, store, "mem 3", []string{"cmd3"}, []string{"tagA", "tagB"})

	// List all
	all, err := store.ListMemories("", 10)
	if err != nil {
		t.Fatalf("ListMemories all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 memories, got %d", len(all))
	}

	// List filtered by tagA (should return id3 and id1 in reverse created order)
	tagAResults, err := store.ListMemories("tagA", 10)
	if err != nil {
		t.Fatalf("ListMemories tagA: %v", err)
	}
	if len(tagAResults) != 2 {
		t.Fatalf("expected 2 memories with tagA, got %d", len(tagAResults))
	}
	if tagAResults[0].ID != id3 || tagAResults[1].ID != id1 {
		t.Errorf("expected [id3, id1], got [%d, %d]", tagAResults[0].ID, tagAResults[1].ID)
	}

	// List filtered by tagB (should return id3 and id2)
	tagBResults, err := store.ListMemories("tagB", 10)
	if err != nil {
		t.Fatalf("ListMemories tagB: %v", err)
	}
	if len(tagBResults) != 2 {
		t.Fatalf("expected 2 memories with tagB, got %d", len(tagBResults))
	}
	if tagBResults[0].ID != id3 || tagBResults[1].ID != id2 {
		t.Errorf("expected [id3, id2], got [%d, %d]", tagBResults[0].ID, tagBResults[1].ID)
	}

	// List with limit
	limited, err := store.ListMemories("", 1)
	if err != nil {
		t.Fatalf("ListMemories limited: %v", err)
	}
	if len(limited) != 1 || limited[0].ID != id3 {
		t.Errorf("expected 1 memory with id3, got %+v", limited)
	}
}
