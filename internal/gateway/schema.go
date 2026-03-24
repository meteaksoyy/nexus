package gateway

import "github.com/meteaksoyy/nexus/internal/graph"

func mustReadSchema() []byte {
	return graph.SchemaBytes
}
