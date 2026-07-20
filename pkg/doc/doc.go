// Package doc embeds the go-go-wm help topics (glaze help system).
package doc

import (
	"embed"

	"github.com/go-go-golems/glazed/pkg/help"
)

//go:embed topics
var docFS embed.FS

// AddDocToHelpSystem loads all embedded topics into hs.
func AddDocToHelpSystem(hs *help.HelpSystem) error {
	return hs.LoadSectionsFromFS(docFS, "topics")
}
