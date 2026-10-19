// Package schema makes the wire contract available to Go code without copying
// it. The JSON file beside this one stays the single source of truth; this
// embeds it so the binary and the tests read the same bytes an integrator does.
package schema

import _ "embed"

// V1 is schema/ivrvg.v1.schema.json, the v1 wire contract.
//
//go:embed ivrvg.v1.schema.json
var V1 []byte
