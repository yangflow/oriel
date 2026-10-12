// Package startup holds the copy of the native dialog shown when the desktop app cannot start its server.
package startup

import (
	_ "embed"
	"encoding/json"

	"github.com/yangflow/oriel/internal/ui/locale"
)

//go:embed copy.json
var catalog []byte

type copy struct{ Title, Locked, Failed string }

var translations = func() map[string]copy {
	var values map[string]copy
	if err := json.Unmarshal(catalog, &values); err != nil {
		panic(err)
	}
	return values
}()

// Message returns the dialog title and detail in the system language. locked means another process
// serves the same data folder; other errors keep their English text after the localized lead, for bug reports.
func Message(systemLocale string, locked bool, err error) (title, detail string) {
	text := translations[locale.Language(systemLocale)]
	if locked {
		return text.Title, text.Locked
	}
	return text.Title, text.Failed + "\n" + err.Error()
}
