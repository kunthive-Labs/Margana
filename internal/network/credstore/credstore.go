// Package credstore stores per-network secrets in the OS keyring under a
// network-namespaced service name ("marga-discord", "marga-matrix", ...), so
// multiple networks' credentials can coexist.
package credstore

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

// legacyService is the un-namespaced service Marga used before multi-network
// support. Discord credentials are migrated forward from here on first load.
const legacyService = "marga"

func service(networkID string) string {
	return "marga-" + networkID
}

// Get returns a stored secret for (networkID, key), or an error if absent.
func Get(networkID, key string) (string, error) {
	return keyring.Get(service(networkID), key)
}

// Set stores a secret for (networkID, key).
func Set(networkID, key, value string) error {
	return keyring.Set(service(networkID), key, value)
}

// Delete removes a stored secret. A missing entry is not an error.
func Delete(networkID, key string) error {
	err := keyring.Delete(service(networkID), key)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}

// MigrateLegacyDiscord copies tokens stored under the old un-namespaced "marga"
// service into the "marga-discord" namespace, so existing users stay logged in.
// It is a no-op once the namespaced entries exist.
func MigrateLegacyDiscord() error {
	for _, key := range []string{"access_token", "refresh_token"} {
		if _, err := Get("discord", key); err == nil {
			continue // already migrated
		}
		legacy, err := keyring.Get(legacyService, key)
		if err != nil {
			continue // nothing to migrate
		}
		if err := Set("discord", key, legacy); err != nil {
			return fmt.Errorf("migrating %s: %w", key, err)
		}
	}
	return nil
}
