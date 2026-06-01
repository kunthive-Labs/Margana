package matrix

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"

	"github.com/kunthive-Labs/Margana/internal/network/credstore"
)

// pickleKeyName is the keyring key under which the crypto pickle key is stored.
const pickleKeyName = "pickle_key"

// cryptoDBPath places the crypto store next to the sync-token cache, under the
// network's state directory.
func cryptoDBPath(syncStorePath string) string {
	return filepath.Join(filepath.Dir(syncStorePath), "crypto.db")
}

// loadOrCreatePickleKey returns the 32-byte key that encrypts the Olm account
// and message sessions at rest, generating and persisting one to the keyring on
// first use. The same key must be reused for a given crypto database, so it
// lives alongside the access token in the OS keyring — never on disk in clear.
func loadOrCreatePickleKey() ([]byte, error) {
	if enc, err := credstore.Get("matrix", pickleKeyName); err == nil && enc != "" {
		if key, err := base64.StdEncoding.DecodeString(enc); err == nil && len(key) > 0 {
			return key, nil
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating pickle key: %w", err)
	}
	if err := credstore.Set("matrix", pickleKeyName, base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, fmt.Errorf("persisting pickle key: %w", err)
	}
	return key, nil
}

// openCryptoDB opens the pure-Go SQLite database backing the crypto store and
// wraps it for mautrix's dbutil. A single writer plus WAL keeps it robust for a
// long-running client without pulling in a CGo SQLite driver.
func openCryptoDB(path string) (*dbutil.Database, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	rawDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	rawDB.SetMaxOpenConns(1)
	db, err := dbutil.NewWithDB(rawDB, "sqlite3")
	if err != nil {
		_ = rawDB.Close()
		return nil, err
	}
	return db, nil
}

// setupCrypto enables end-to-end encryption on the client. It opens the crypto
// store, initializes the Olm machine via mautrix's cryptohelper, and attaches
// it to the client so that:
//   - encrypted rooms are decrypted during /sync (the helper re-dispatches the
//     plaintext event through the normal handlers), and
//   - outgoing messages to encrypted rooms are encrypted automatically.
//
// The client must already be logged in (UserID and DeviceID set). The returned
// helper should be Closed on disconnect.
func setupCrypto(ctx context.Context, client *mautrix.Client, dbPath string) (*cryptohelper.CryptoHelper, error) {
	pickleKey, err := loadOrCreatePickleKey()
	if err != nil {
		return nil, err
	}
	db, err := openCryptoDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening crypto store: %w", err)
	}
	helper, err := cryptohelper.NewCryptoHelper(client, pickleKey, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating crypto helper: %w", err)
	}
	if err := helper.Init(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing crypto: %w", err)
	}
	client.Crypto = helper
	return helper, nil
}
