package gateway

import (
	_ "embed"
)

//go:embed ../graph/schema.graphql
var schemaFile []byte

func mustReadSchema() []byte {
	return schemaFile
}
