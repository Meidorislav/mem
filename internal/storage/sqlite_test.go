package storage

import (
	"errors"
	"path/filepath"
	"reflect"
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

func TestEmbeddingStatusNullCommandUpsert(t *testing.T) {
	store := newTestStore(t)
	memID := saveTestMemory(t, store, "context only", nil, nil)
	ec, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 10)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}

	for _, hash := range []string{"hash1", "hash2"} {
		st := &EmbeddingStatus{MemoryID: memID, EmbeddingConfigID: ec.ID, ContentHash: hash}
		if err := store.UpsertEmbeddingStatus(st); err != nil {
			t.Fatalf("UpsertEmbeddingStatus(%s): %v", hash, err)
		}
	}

	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM embedding_status WHERE memory_id = ?", memID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("status rows = %d, want 1", count)
	}

	st, err := store.GetEmbeddingStatus(memID, nil, 0, ec.ID)
	if err != nil || st == nil {
		t.Fatalf("GetEmbeddingStatus: %v, %v", st, err)
	}
	if st.ContentHash != "hash2" {
		t.Errorf("ContentHash = %q, want hash2", st.ContentHash)
	}
}

func TestActivateEmbeddingConfig(t *testing.T) {
	store := newTestStore(t)

	active, err := store.GetActiveEmbeddingConfig()
	if err != nil || active != nil {
		t.Fatalf("GetActiveEmbeddingConfig on empty db = %v, %v; want nil, nil", active, err)
	}

	first, err := store.GetOrCreateActiveEmbeddingConfig("model-a", 768)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	memID := saveTestMemory(t, store, "indexed", nil, nil)
	if err := store.UpsertEmbeddingStatus(&EmbeddingStatus{MemoryID: memID, EmbeddingConfigID: first.ID, ContentHash: "h"}); err != nil {
		t.Fatalf("UpsertEmbeddingStatus: %v", err)
	}

	second, err := store.ActivateEmbeddingConfig("model-b", 1024)
	if err != nil {
		t.Fatalf("ActivateEmbeddingConfig: %v", err)
	}
	if second.ID == first.ID || !second.IsActive {
		t.Errorf("second config = %+v, want new active config", second)
	}

	active, err = store.GetActiveEmbeddingConfig()
	if err != nil || active == nil || active.ID != second.ID {
		t.Fatalf("GetActiveEmbeddingConfig = %+v, %v; want model-b", active, err)
	}

	st, err := store.GetEmbeddingStatus(memID, nil, 0, first.ID)
	if err != nil || st == nil {
		t.Fatalf("GetEmbeddingStatus: %v, %v", st, err)
	}
	if !st.NeedsReindex {
		t.Error("NeedsReindex = false after model switch, want true")
	}

	// Switching back reuses the existing config row.
	again, err := store.ActivateEmbeddingConfig("model-a", 768)
	if err != nil {
		t.Fatalf("ActivateEmbeddingConfig (back): %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("reactivated ID = %d, want %d", again.ID, first.ID)
	}
	active, _ = store.GetActiveEmbeddingConfig()
	if active.ID != first.ID {
		t.Errorf("active ID = %d, want %d", active.ID, first.ID)
	}
}

func TestAllMemoryIDs(t *testing.T) {
	store := newTestStore(t)
	id1 := saveTestMemory(t, store, "one", nil, nil)
	id2 := saveTestMemory(t, store, "two", nil, nil)

	ids, err := store.AllMemoryIDs()
	if err != nil {
		t.Fatalf("AllMemoryIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != id1 || ids[1] != id2 {
		t.Errorf("ids = %v, want [%d %d]", ids, id1, id2)
	}
}

func TestCountUnindexedMemories(t *testing.T) {
	store := newTestStore(t)
	ec, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 3)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	indexed := saveTestMemory(t, store, "indexed", nil, nil)
	saveTestMemory(t, store, "not indexed", nil, nil)
	if err := store.UpsertEmbeddingStatus(&EmbeddingStatus{MemoryID: indexed, EmbeddingConfigID: ec.ID, ContentHash: "h"}); err != nil {
		t.Fatalf("UpsertEmbeddingStatus: %v", err)
	}

	n, err := store.CountUnindexedMemories(ec.ID)
	if err != nil || n != 1 {
		t.Fatalf("CountUnindexedMemories = %d, %v; want 1", n, err)
	}

	if err := store.MarkAllForReindex(); err != nil {
		t.Fatalf("MarkAllForReindex: %v", err)
	}
	if n, _ := store.CountUnindexedMemories(ec.ID); n != 2 {
		t.Errorf("after MarkAllForReindex = %d, want 2", n)
	}
}

func TestSetEmbeddingScheme(t *testing.T) {
	store := newTestStore(t)
	ec, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 3)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	memID := saveTestMemory(t, store, "indexed", nil, nil)
	if err := store.UpsertEmbeddingStatus(&EmbeddingStatus{MemoryID: memID, EmbeddingConfigID: ec.ID, ContentHash: "h"}); err != nil {
		t.Fatalf("UpsertEmbeddingStatus: %v", err)
	}

	if err := store.SetEmbeddingScheme(ec.ID, "v2"); err != nil {
		t.Fatalf("SetEmbeddingScheme: %v", err)
	}

	active, err := store.GetActiveEmbeddingConfig()
	if err != nil || active == nil || active.Version == nil || *active.Version != "v2" {
		t.Fatalf("active config = %+v, %v; want version v2", active, err)
	}
	st, _ := store.GetEmbeddingStatus(memID, nil, 0, ec.ID)
	if st == nil || !st.NeedsReindex {
		t.Errorf("status = %+v, want NeedsReindex", st)
	}
}

func TestUpdateMemory(t *testing.T) {
	store := newTestStore(t)
	id := saveTestMemory(t, store, "old title", []string{"echo old", "rm -rf build"}, []string{"a", "b"})
	other := saveTestMemory(t, store, "untouched", []string{"echo other"}, []string{"a"})

	ec, err := store.GetOrCreateActiveEmbeddingConfig("test-model", 3)
	if err != nil {
		t.Fatalf("GetOrCreateActiveEmbeddingConfig: %v", err)
	}
	for _, mid := range []int64{id, other} {
		if err := store.UpsertEmbeddingStatus(&EmbeddingStatus{MemoryID: mid, EmbeddingConfigID: ec.ID, ContentHash: "h"}); err != nil {
			t.Fatalf("UpsertEmbeddingStatus: %v", err)
		}
	}

	m := &Memory{
		ID:          id,
		Title:       "new title",
		Description: "now with a description",
		Commands:    []Command{{Command: "echo new"}},
		Tags:        []string{"c", "c"},
	}
	if err := store.UpdateMemory(m); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}

	got, err := store.GetMemory(id)
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Title != "new title" || got.Description != "now with a description" {
		t.Errorf("memory = %+v, want updated title and description", got)
	}
	if len(got.Commands) != 1 || got.Commands[0].Command != "echo new" || got.Commands[0].Position != 0 {
		t.Errorf("Commands = %+v, want [echo new]", got.Commands)
	}
	if !reflect.DeepEqual(got.Tags, []string{"c"}) {
		t.Errorf("Tags = %v, want [c]", got.Tags)
	}

	// Edited memory needs reindexing; the other one is untouched.
	if n, _ := store.CountUnindexedMemories(ec.ID); n != 1 {
		t.Errorf("unindexed = %d, want 1", n)
	}

	// Keyword index follows the edit.
	if ids, _ := store.KeywordSearch("old", 10); len(ids) != 0 {
		t.Errorf("old text still found: %v", ids)
	}
	if ids, _ := store.KeywordSearch("description", 10); !reflect.DeepEqual(ids, []int64{id}) {
		t.Errorf("KeywordSearch(description) = %v, want [%d]", ids, id)
	}

	if err := store.UpdateMemory(&Memory{ID: 999, Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateMemory(999) err = %v, want ErrNotFound", err)
	}
}

func TestFindDuplicate(t *testing.T) {
	store := newTestStore(t)
	id := saveTestMemory(t, store, "list files by size", []string{"ls -lhS"}, nil)
	saveTestMemory(t, store, "list files by size", []string{"ls -lhS", "du -sh"}, nil)

	tests := []struct {
		title    string
		commands []string
		want     int64
	}{
		{"list files by size", []string{"ls -lhS"}, id},
		{"list files by size", []string{"ls -lh"}, 0},
		{"list files", []string{"ls -lhS"}, 0},
		{"list files by size", nil, 0},
	}
	for _, tt := range tests {
		got, err := store.FindDuplicate(tt.title, tt.commands)
		if err != nil {
			t.Fatalf("FindDuplicate: %v", err)
		}
		if got != tt.want {
			t.Errorf("FindDuplicate(%q, %q) = %d, want %d", tt.title, tt.commands, got, tt.want)
		}
	}
}
