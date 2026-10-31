// Package migrationfixture supplies isolated, synthetic migration test inputs.
package migrationfixture

import (
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"testing/fstest"

	"personaldesk/migrations"
)

//go:embed frozen-v1.sql
var SQL string

//go:embed frozen-v2.sql
var SQLV2 string

func Populate(path string) error {
	return populate(path, SQL)
}
func PopulateV2(path string) error {
	return populate(path, SQLV2)
}
func populate(path, script string) error {
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(script)
	return err
}
func Catalog(scripts ...string) (*migrations.Catalog, error) {

	catalog, err := migrations.Embedded()
	if err != nil {
		return nil, err
	}
	files := fstest.MapFS{}
	manifest := migrations.Manifest{Version: 1, Migrations: catalog.Entries()}
	for _, entry := range catalog.Entries() {
		data, err := migrations.Files.ReadFile(entry.File)
		if err != nil {
			return nil, err
		}
		files[entry.File] = &fstest.MapFile{Data: data}
	}
	for i, script := range scripts {
		version := catalog.Latest() + int64(i) + 1
		name := fmt.Sprintf("%05d_test_step.sql", version)
		files[name] = &fstest.MapFile{Data: []byte(script)}
		sum := sha256.Sum256([]byte(script))
		manifest.Migrations = append(manifest.Migrations, migrations.Entry{Version: version, File: name, SHA256: hex.EncodeToString(sum[:])})
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return migrations.NewCatalog(files, data)
}
