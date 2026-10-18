// Package icons holds the one icon set of the app: Lucide (ISC License), lucide-static v1.59.0, files unchanged.
// Show them with ui.Icon, which colors and sizes them as text.
package icons

import (
	"embed"

	"github.com/egoist/mygo/ui"
)

//go:embed lucide/*.svg
var files embed.FS

func parse(name string) *ui.SVG {
	data, err := files.ReadFile("lucide/" + name + ".svg")
	if err != nil {
		panic(err)
	}
	return ui.MustParseSVG(data)
}

var (
	ArrowRight   = parse("arrow-right")
	Check        = parse("check")
	CircleAlert  = parse("circle-alert")
	Clock        = parse("clock")
	Database     = parse("database")
	Eye          = parse("eye")
	EyeOff       = parse("eye-off")
	Globe        = parse("globe")
	KeyRound     = parse("key-round")
	Laptop       = parse("laptop")
	LoaderCircle = parse("loader-circle")
	LockKeyhole  = parse("lock-keyhole")
	Server       = parse("server")
	WifiOff      = parse("wifi-off")
)
