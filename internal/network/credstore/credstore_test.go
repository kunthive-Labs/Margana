package credstore

import (
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSetGetDeleteNamespacing(t *testing.T) {
	keyring.MockInit()

	if err := Set("matrix", "access_token", "mtoken"); err != nil {
		t.Fatalf("Set matrix: %v", err)
	}
	if err := Set("discord", "access_token", "dtoken"); err != nil {
		t.Fatalf("Set discord: %v", err)
	}

	// Same key, different networks must not collide.
	if got, _ := Get("matrix", "access_token"); got != "mtoken" {
		t.Errorf("matrix token = %q, want mtoken", got)
	}
	if got, _ := Get("discord", "access_token"); got != "dtoken" {
		t.Errorf("discord token = %q, want dtoken", got)
	}

	// The namespaced service name is "marga-<network>".
	if got, _ := keyring.Get("marga-matrix", "access_token"); got != "mtoken" {
		t.Errorf("expected entry under marga-matrix, got %q", got)
	}

	if err := Delete("matrix", "access_token"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Get("matrix", "access_token"); err == nil {
		t.Error("expected error after Delete")
	}
}

func TestDeleteMissingIsNotError(t *testing.T) {
	keyring.MockInit()
	if err := Delete("matrix", "never_set"); err != nil {
		t.Errorf("deleting a missing entry should be a no-op, got %v", err)
	}
}

func TestMigrateLegacyDiscord(t *testing.T) {
	keyring.MockInit()

	// Simulate a pre-multi-network install: tokens under the bare "marga" service.
	if err := keyring.Set(legacyService, "access_token", "legacy-access"); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	if err := keyring.Set(legacyService, "refresh_token", "legacy-refresh"); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	if err := MigrateLegacyDiscord(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if got, _ := Get("discord", "access_token"); got != "legacy-access" {
		t.Errorf("access_token not migrated: %q", got)
	}
	if got, _ := Get("discord", "refresh_token"); got != "legacy-refresh" {
		t.Errorf("refresh_token not migrated: %q", got)
	}
}

func TestMigrateLegacyDiscordDoesNotClobber(t *testing.T) {
	keyring.MockInit()

	// Already migrated: namespaced entry exists and must win over legacy.
	if err := Set("discord", "access_token", "current"); err != nil {
		t.Fatalf("seed current: %v", err)
	}
	if err := keyring.Set(legacyService, "access_token", "stale-legacy"); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}

	if err := MigrateLegacyDiscord(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if got, _ := Get("discord", "access_token"); got != "current" {
		t.Errorf("migration clobbered existing token: %q", got)
	}
}
