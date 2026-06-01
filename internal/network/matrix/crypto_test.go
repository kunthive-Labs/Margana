package matrix

import (
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestCryptoDBPath(t *testing.T) {
	got := cryptoDBPath("/data/marga/matrix/sync.json")
	want := filepath.FromSlash("/data/marga/matrix/crypto.db")
	if got != want {
		t.Errorf("cryptoDBPath = %q, want %q", got, want)
	}
}

func TestLoadOrCreatePickleKeyIsStable(t *testing.T) {
	keyring.MockInit()

	first, err := loadOrCreatePickleKey()
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("pickle key length = %d, want 32", len(first))
	}

	// A second call must return the persisted key, not a fresh one — otherwise
	// the on-disk crypto store would become undecryptable after a restart.
	second, err := loadOrCreatePickleKey()
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if string(first) != string(second) {
		t.Error("pickle key changed between calls; it must be stable")
	}
}

func TestOpenCryptoDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "crypto.db")
	db, err := openCryptoDB(path)
	if err != nil {
		t.Fatalf("openCryptoDB: %v", err)
	}
	defer db.Close()

	// The pure-Go SQLite driver and dbutil dialect must actually work together.
	if err := db.RawDB.Ping(); err != nil {
		t.Errorf("ping crypto db: %v", err)
	}
	if _, err := db.RawDB.Exec("CREATE TABLE t (x INTEGER)"); err != nil {
		t.Errorf("exec on crypto db: %v", err)
	}
}
