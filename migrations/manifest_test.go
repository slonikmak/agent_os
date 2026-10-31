package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestEmbeddedMigrationsMatchFrozenManifest(t *testing.T) {
	data, err := os.ReadFile("../docs/migration-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Migrations []struct {
			Version int64  `json:"version"`
			File    string `json:"file"`
			SHA256  string `json:"sha256"`
		} `json:"migrations"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	entries, err := Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(entries)) != Latest || len(entries) != len(manifest.Migrations) {
		t.Fatal("packaged migration set differs from manifest")
	}
	for i, migration := range manifest.Migrations {
		if migration.Version != int64(i+1) {
			t.Fatal("non-prefix migration manifest")
		}
		sql, err := Files.ReadFile(migration.File)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(sql)
		if hex.EncodeToString(sum[:]) != migration.SHA256 {
			t.Fatal("frozen SQL hash changed")
		}
	}
}

func TestReleasedV2SQLIsImmutable(t *testing.T) {
	data, err := Files.ReadFile("00002_durable_inbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "3499e1e1b97503dcdd1f39eb0e2aefda70f7112363763ab49b4d34d16834770d" {
		t.Fatal("released migration2 changed")
	}
}

func TestReleasedV3SQLIsImmutable(t *testing.T) {
	data, err := Files.ReadFile("00003_create_task_from_inbox.sql")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != "ababfac611159f580ded2f7ee97686d5bf31875157e288de264b9a1f97c733f3" {
		t.Fatal("released migration3 changed")
	}
}
