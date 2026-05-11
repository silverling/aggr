package server

import _ "embed"

//go:embed internal/webui/dist/index.html
var embeddedIndexHTML string
