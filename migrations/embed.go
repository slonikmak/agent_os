package migrations

import "embed"

//go:embed *.sql
var Files embed.FS

//go:embed manifest.json
var manifest []byte

const Latest int64 = 4
