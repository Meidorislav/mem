package storage

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestFTSQuery(t *testing.T) {
	tests := []struct{ in, want string }{
		{"how did I fix the nginx 502?", `"fix"* OR "nginx"* OR "502"*`},
		{"как я чинил 502 в nginx", `"чинил"* OR "502"* OR "nginx"*`},
		{`docker "run" --rm) OR NEAR(x`, `"docker"* OR "run"* OR "rm"* OR "near"*`},
		{"the a и в", ""},
		{"ECONNREFUSED econnrefused", `"econnrefused"*`},
	}
	for _, tt := range tests {
		if got := ftsQuery(tt.in); got != tt.want {
			t.Errorf("ftsQuery(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestKeywordSearch(t *testing.T) {
	store := newTestStore(t)
	nginx := saveTestMemory(t, store, "Починил 502 на проде",
		[]string{"tail -n 100 /var/log/nginx/error.log", "systemctl restart nginx"}, []string{"nginx"})
	docker := saveTestMemory(t, store, "docker dns",
		[]string{"docker run --rm alpine cat /etc/resolv.conf"}, nil)
	conn := saveTestMemory(t, store, "api down",
		[]string{"curl localhost:8080 # ECONNREFUSED"}, []string{"api"})

	tests := []struct {
		query string
		want  []int64
	}{
		{"как я чинил 502", []int64{nginx}},
		{"починил", []int64{nginx}},
		{"--rm", []int64{docker}},
		{"econnrefused", []int64{conn}},
		{"resolv", []int64{docker}}, // prefix of resolv.conf
		{"как это было", nil},       // only stop words
	}
	for _, tt := range tests {
		got, err := store.KeywordSearch(tt.query, 10)
		if err != nil {
			t.Fatalf("KeywordSearch(%q): %v", tt.query, err)
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("KeywordSearch(%q) = %v, want %v", tt.query, got, tt.want)
		}
	}
}

func TestKeywordIndexFollowsDeletes(t *testing.T) {
	store := newTestStore(t)
	id := saveTestMemory(t, store, "temporary note", []string{"echo unique-marker"}, nil)
	if err := store.DeleteMemory(id); err != nil {
		t.Fatalf("DeleteMemory: %v", err)
	}
	if got, _ := store.KeywordSearch("marker", 10); len(got) != 0 {
		t.Errorf("deleted memory still found: %v", got)
	}
}

func TestKeywordIndexBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	store, err := NewStoreAt(path)
	if err != nil {
		t.Fatalf("NewStoreAt: %v", err)
	}
	id := saveTestMemory(t, store, "old memory", []string{"journalctl -u api"}, []string{"ops"})

	// Simulate a database created before keyword search existed.
	if _, err := store.db.Exec("DELETE FROM memory_fts"); err != nil {
		t.Fatal(err)
	}
	store.Close()

	store, err = NewStoreAt(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	for _, q := range []string{"journalctl", "ops", "old"} {
		if got, _ := store.KeywordSearch(q, 10); !reflect.DeepEqual(got, []int64{id}) {
			t.Errorf("after backfill KeywordSearch(%q) = %v, want [%d]", q, got, id)
		}
	}
}
