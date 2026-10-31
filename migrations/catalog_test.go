package migrations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
	"testing/fstest"
)

func TestEmbeddedCatalogAndDocumentedManifestAgree(t *testing.T) {
	c, err := Embedded()
	if err != nil {
		t.Fatal(err)
	}
	if c.Latest() != Latest || len(c.Entries()) != int(Latest) || c.Identity() != c.PrefixIdentity(Latest) {
		t.Fatal("unexpected production catalog")
	}
	documented, err := os.ReadFile("../docs/migration-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(documented, manifest) {
		t.Fatal("documented and embedded manifests differ")
	}
}
func TestInvalidCatalogsRefused(t *testing.T) {
	initial, err := Files.ReadFile("00001_initial_schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, script string
		version      int64
		file         string
	}{
		{"nontransactional", "-- +goose NO TRANSACTION\n-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"spaced-directive", "-- +goose NO  TRANSACTION\n-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"spaced-commit", "-- +goose Up\nCREATE TABLE unsafe(value TEXT);\nCOMMIT \n;\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"implicit-end", "-- +goose Up\nCREATE TABLE unsafe(value TEXT);\nEND;\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"file-side-effect", "-- +goose Up\nVACUUM INTO '/unsafe';\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"no-up", "SELECT 1;\n-- +goose Down\nSELECT 1;\n", 2, "00002_step.sql"},
		{"unordered", "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n", 1, "00001_step.sql"},
		{"version-drift", "-- +goose Up\nSELECT 1;\n-- +goose Down\nSELECT 1;\n", 3, "00002_step.sql"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var m Manifest
			_ = json.Unmarshal(manifest, &m)
			m.Migrations = m.Migrations[:1] // independent frozen-v1 prefix
			sum := sha256.Sum256([]byte(test.script))
			m.Migrations = append(m.Migrations, Entry{test.version, test.file, hex.EncodeToString(sum[:])})
			data, _ := json.Marshal(m)
			fs := fstest.MapFS{"00001_initial_schema.sql": &fstest.MapFile{Data: initial}, test.file: &fstest.MapFile{Data: []byte(test.script)}}
			if _, err = NewCatalog(fs, data); err == nil {
				t.Fatal("accepted invalid catalog")
			}
		})
	}
	var m Manifest
	_ = json.Unmarshal(manifest, &m)
	m.Migrations = m.Migrations[:1]
	changed := append(append([]byte(nil), initial...), []byte("\n-- changed released file\n")...)
	sum := sha256.Sum256(changed)
	m.Migrations[0].SHA256 = hex.EncodeToString(sum[:])
	data, _ := json.Marshal(m)
	if _, err = NewCatalog(fstest.MapFS{"00001_initial_schema.sql": &fstest.MapFile{Data: changed}}, data); err == nil {
		t.Fatal("rewrote released identity")
	}
	if _, err = NewCatalog(Files, append(manifest, []byte(" {}")...)); err == nil {
		t.Fatal("accepted trailing manifest data")
	}
}
