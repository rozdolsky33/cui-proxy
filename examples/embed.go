// Package examples embeds the starter config shipped with cui-proxy, so
// `cui-proxy init` can write it without needing the source tree.
package examples

import "embed"

// FS holds config.yaml and hosts.txt.
//
//go:embed config.yaml hosts.txt
var FS embed.FS
