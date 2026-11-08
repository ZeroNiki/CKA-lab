// Package web holds the browser UI, embedded into the server binary.
package web

import "embed"

//go:embed *.html
var FS embed.FS
