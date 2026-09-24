package outbox

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"
)

// released pins the sha256 of every migration file by name. A released file
// never changes in text or name: a new migration adds a row here.
var released = map[string]string{
	"0001_messaging.up.sql":   "4521d0ab21d45a424041bc6432d8bd44dfc43c439d200c20ceb58923758d1b9d",
	"0001_messaging.down.sql": "e2474958b549a4c3e5265e64770fc4b7229cccf0cee478cc3eb2283f25b831a9",
}

func TestGoldenHashes(t *testing.T) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		text, err := fs.ReadFile(migrationFiles, "migrations/"+e.Name())
		if err != nil {
			t.Fatalf("ReadFile %s: %v", e.Name(), err)
		}
		sum := sha256.Sum256(text)
		got := hex.EncodeToString(sum[:])
		want, ok := released[e.Name()]
		switch {
		case !ok:
			t.Errorf("%s is not in the released table; add it with hash %s", e.Name(), got)
		case got != want:
			t.Errorf("%s changed: hash %s, released %s", e.Name(), got, want)
		}
		seen[e.Name()] = true
	}
	for name := range released {
		if !seen[name] {
			t.Errorf("released file %s is missing from the migrations directory", name)
		}
	}
}
