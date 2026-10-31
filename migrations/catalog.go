package migrations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
)

var ErrCatalog = errors.New("invalid migration catalog")

type Entry struct {
	Version int64  `json:"version"`
	File    string `json:"file"`
	SHA256  string `json:"sha256"`
}
type Manifest struct {
	Version    int     `json:"version"`
	Migrations []Entry `json:"migrations"`
}

// Catalog is a validated immutable snapshot. Its identity binds recovery records
// to an exact ordered chain, rather than to a mutable application version.
type Catalog struct {
	files    fs.FS
	entries  []Entry
	identity string
}

func Embedded() (*Catalog, error)   { return NewCatalog(Files, manifest) }
func (c *Catalog) Files() fs.FS     { return c.files }
func (c *Catalog) Entries() []Entry { return append([]Entry(nil), c.entries...) }
func (c *Catalog) Latest() int64    { return c.entries[len(c.entries)-1].Version }
func (c *Catalog) Identity() string { return c.identity }
func (c *Catalog) PrefixIdentity(version int64) string {
	var entries []Entry
	for _, e := range c.entries {
		if e.Version <= version {
			entries = append(entries, e)
		}
	}
	data, _ := json.Marshal(entries)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

var migrationName = regexp.MustCompile(`^([0-9]{5,})_[a-z0-9_]+\.sql$`)

// NewCatalog is also used by private future-catalog tests. Released identities
// are pinned independently here; production never adds synthetic migrations.
func NewCatalog(files fs.FS, data []byte) (*Catalog, error) {
	var m Manifest
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Version != 1 || len(m.Migrations) == 0 {
		return nil, ErrCatalog
	}
	paths, err := fs.Glob(files, "*.sql")
	if err != nil || len(paths) != len(m.Migrations) {
		return nil, ErrCatalog
	}
	var previous int64
	for i, e := range m.Migrations {
		match := migrationName.FindStringSubmatch(e.File)
		if len(match) != 2 || paths[i] != e.File || e.Version <= previous {
			return nil, ErrCatalog
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version != e.Version {
			return nil, ErrCatalog
		}
		data, err := fs.ReadFile(files, e.File)
		if err != nil {
			return nil, ErrCatalog
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != e.SHA256 {
			return nil, ErrCatalog
		}
		text := strings.ToUpper(string(data))
		if !strings.Contains(text, "-- +GOOSE UP\n") || !strings.Contains(text, "-- +GOOSE DOWN\n") {
			return nil, ErrCatalog
		}
		// SQLite transaction escapes and connection/filesystem effects have no
		// place in a schema script. Goose owns each transaction and its ledger.
		for _, forbidden := range []string{"NO TRANSACTION", "PRAGMA", "ATTACH", "DETACH", "VACUUM", "LOAD_EXTENSION", "WRITABLE_SCHEMA", "GOOSE_DB_VERSION", "BEGIN TRANSACTION", "COMMIT;", "ROLLBACK;"} {
			if strings.Contains(text, forbidden) {
				return nil, ErrCatalog
			}
		}
		if !transactionalSQL(string(data)) {
			return nil, ErrCatalog
		}
		if i == 0 && (e.Version != 1 || e.File != "00001_initial_schema.sql" || e.SHA256 != "f07d0ee79ec6ed34a588e922cad119eb9d364c68482eb5581c8f0e0ea54c1293") {
			return nil, ErrCatalog
		}
		previous = e.Version
	}
	canonical, _ := json.Marshal(m.Migrations)
	sum := sha256.Sum256(canonical)
	return &Catalog{files: files, entries: append([]Entry(nil), m.Migrations...), identity: hex.EncodeToString(sum[:])}, nil
}

// Goose must own transaction boundaries. Inspect statement starts after
// removing comments/quoted content, so "COMMIT ;" and split-line transaction
// controls cannot escape that boundary. SQL strings and trigger bodies retain
// their normal semicolon behavior. The runner remains the final SQL parser.
func transactionalSQL(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(strings.ToUpper(strings.Join(strings.Fields(line), " ")), "+GOOSE NO TRANSACTION") {
			return false
		}
	}
	var plain strings.Builder
	for i := 0; i < len(text); {
		if i+1 < len(text) && text[i:i+2] == "--" {
			for i < len(text) && text[i] != '\n' {
				i++
			}
			plain.WriteByte(' ')
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "/*" {
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return false
			}
			i += end + 4
			plain.WriteByte(' ')
			continue
		}
		if text[i] == '\'' || text[i] == '"' || text[i] == '`' || text[i] == '[' {
			end := text[i]
			if end == '[' {
				end = ']'
			}
			i++
			closed := false
			for i < len(text) {
				if text[i] == end {
					if i+1 < len(text) && text[i+1] == end {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return false
			}
			plain.WriteString(" quoted ")
			continue
		}
		plain.WriteByte(text[i])
		i++
	}
	inTrigger := false
	for _, statement := range strings.Split(strings.ToUpper(plain.String()), ";") {
		words := strings.Fields(statement)
		if len(words) == 0 {
			continue
		}
		switch words[0] {
		case "CREATE":
			if len(words) > 1 && (words[1] == "TRIGGER" || len(words) > 2 && (words[1] == "TEMP" || words[1] == "TEMPORARY") && words[2] == "TRIGGER") {
				inTrigger = true
			}
		case "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "REPLACE", "SELECT", "WITH":
		case "END":
			if !inTrigger || len(words) != 1 {
				return false
			}
			inTrigger = false
		default:
			return false
		}
	}
	return !inTrigger
}
