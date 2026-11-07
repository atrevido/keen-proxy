package journal

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestVisitSnapshotsRepetitionsWithoutBlockingWriters(t *testing.T) {
	j := New(Options{File: filepath.Join(t.TempDir(), "system.jsonl"), Dedup: true})
	defer j.Close()
	now := time.Now()
	j.Add(Entry{Message: "repeat", Time: now})
	j.Add(Entry{Message: "repeat", Time: now.Add(time.Second)})
	j.Add(Entry{Message: "other", Time: now.Add(2 * time.Second)})
	var entries []Entry
	err := j.Visit(func(e Entry) error {
		entries = append(entries, e)
		if len(entries) == 1 {
			j.Add(Entry{Message: "new", Time: now.Add(3 * time.Second)})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != 1 || entries[0].Count != 2 || entries[1].ID != 2 {
		t.Fatalf("unexpected snapshot: %+v", entries)
	}
}

func TestVisitSkipsOverlongRowsAndPreservesFollowingHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	data := strings.Repeat("x", 2<<20) + "\n" + `{"id":12,"action":"kept"}` + "\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	j := New(Options{File: path, MaxBytes: 4 << 20})
	defer j.Close()
	j.Add(Entry{Action: "new"})
	var ids []uint64
	if err := j.Visit(func(e Entry) error {
		ids = append(ids, e.ID)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids, []uint64{12, 13}) {
		t.Fatalf("retained IDs = %v", ids)
	}
}

func TestVisitMergesMemoryAndDiskRetention(t *testing.T) {
	for _, tc := range []struct {
		name     string
		memory   int
		maxBytes int64
	}{
		{"memory outlives rotation", 10, 1},
		{"disk outlives memory", 1, 1 << 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := New(Options{File: filepath.Join(t.TempDir(), "routes.jsonl"), Memory: tc.memory, MaxBytes: tc.maxBytes})
			defer j.Close()
			for range 4 {
				j.Add(Entry{Host: "example.test"})
			}
			var ids []uint64
			if err := j.Visit(func(e Entry) error {
				ids = append(ids, e.ID)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(ids, []uint64{1, 2, 3, 4}) {
				t.Fatalf("retained IDs = %v", ids)
			}
		})
	}
}
