package server

import _ "embed"

// embeddedIndexHTML holds the production UI HTML that gets embedded into the binary.
//
//go:embed internal/webui/dist/index.html
var embeddedIndexHTML string
