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
	ArrowRight     = parse("arrow-right")
	ArrowUp        = parse("arrow-up")
	Check          = parse("check")
	CheckCheck     = parse("check-check")
	CircleAlert    = parse("circle-alert")
	CircleCheckBig = parse("circle-check-big")
	CircleDot      = parse("circle-dot")
	Clock          = parse("clock")
	Database       = parse("database")
	ExternalLink   = parse("external-link")
	Eye            = parse("eye")
	EyeOff         = parse("eye-off")
	Globe          = parse("globe")
	Image          = parse("image")
	Inbox          = parse("inbox")
	KeyRound       = parse("key-round")
	Laptop         = parse("laptop")
	LoaderCircle   = parse("loader-circle")
	LockKeyhole    = parse("lock-keyhole")
	MessageCircle  = parse("message-circle")
	Newspaper      = parse("newspaper")
	Play           = parse("play")
	RefreshCw      = parse("refresh-cw")
	Rss            = parse("rss")
	Search         = parse("search")
	Server         = parse("server")
	Star           = parse("star")
	WifiOff        = parse("wifi-off")
	X              = parse("x")
)
