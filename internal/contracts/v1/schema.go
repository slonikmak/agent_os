package v1

import (
	"bytes"
	"embed"
	"encoding/json"
	"io"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"personaldesk/internal/contracts"
)

// Schemas and Examples are the sole published resources; documentation links to
// these files rather than keeping a second schema/fixture inventory.
//
//go:embed schemas/*.json
var Schemas embed.FS

//go:embed examples/*.json
var Examples embed.FS

const schemaBase = "https://personaldesk.invalid/contracts/v1/"

var schemaCache = sync.OnceValues(compileSchemas)

type embeddedOnly struct{}

func (embeddedOnly) Load(string) (any, error) { return nil, contracts.ErrInvalid }

func compileSchemas() (map[string]*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	compiler.UseLoader(embeddedOnly{})
	entries, err := Schemas.ReadDir("schemas")
	if err != nil {
		return nil, contracts.ErrInvalid
	}
	for _, entry := range entries {
		data, err := Schemas.ReadFile("schemas/" + entry.Name())
		if err != nil {
			return nil, contracts.ErrInvalid
		}
		value, _, err := contracts.Parse(bytes.NewReader(data), MaxResponseBytes)
		if err != nil {
			return nil, contracts.ErrInvalid
		}
		if err = compiler.AddResource(schemaBase+entry.Name(), value); err != nil {
			return nil, contracts.ErrInvalid
		}
	}
	result := map[string]*jsonschema.Schema{}
	for _, entry := range entries {
		schema, err := compiler.Compile(schemaBase + entry.Name())
		if err != nil {
			return nil, err
		}
		result[entry.Name()] = schema
	}
	return result, nil
}

// Validate rejects errors with a fixed value, never a validator description that
// could reflect a private input value. All schema resolution is embedded only.
func Validate(name string, r io.Reader, limit int64) ([]byte, error) {
	value, data, err := contracts.Parse(r, limit)
	if err != nil {
		return nil, err
	}
	schemas, err := schemaCache()
	if err != nil {
		return nil, contracts.ErrInvalid
	}
	schema, ok := schemas[name+".schema.json"]
	if !ok || schema.Validate(value) != nil {
		return nil, contracts.ErrInvalid
	}
	return data, nil
}

// Decode returns a zero value on every failure; no partially populated command
// escapes validation. Presence fields preserve omitted/null/value at this layer.
func Decode[T any](name string, r io.Reader, limit int64) (T, error) {
	var result T
	data, err := Validate(name, r, limit)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(data, &result); err != nil {
		var zero T
		return zero, contracts.ErrInvalid
	}
	return result, nil
}
