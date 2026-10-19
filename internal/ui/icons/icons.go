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
	ArrowUpDown    = parse("arrow-up-down")
	Check          = parse("check")
	CheckCheck     = parse("check-check")
	ChevronDown    = parse("chevron-down")
	ChevronLeft    = parse("chevron-left")
	ChevronUp      = parse("chevron-up")
	CircleAlert    = parse("circle-alert")
	CircleCheckBig = parse("circle-check-big")
	CircleDot      = parse("circle-dot")
	Clock          = parse("clock")
	Database       = parse("database")
	Download       = parse("download")
	ExternalLink   = parse("external-link")
	Eye            = parse("eye")
	EyeOff         = parse("eye-off")
	FileText       = parse("file-text")
	Globe          = parse("globe")
	Image          = parse("image")
	Inbox          = parse("inbox")
	KeyRound       = parse("key-round")
	Languages      = parse("languages")
	Laptop         = parse("laptop")
	Link           = parse("link")
	LoaderCircle   = parse("loader-circle")
	LockKeyhole    = parse("lock-keyhole")
	MessageCircle  = parse("message-circle")
	Monitor        = parse("monitor")
	Newspaper      = parse("newspaper")
	Pause          = parse("pause")
	Pencil         = parse("pencil")
	Play           = parse("play")
	Plug           = parse("plug")
	Plus           = parse("plus")
	RefreshCw      = parse("refresh-cw")
	RotateCcw      = parse("rotate-ccw")
	Rss            = parse("rss")
	Search         = parse("search")
	Server         = parse("server")
	Settings       = parse("settings")
	Smartphone     = parse("smartphone")
	Sparkles       = parse("sparkles")
	Star           = parse("star")
	Trash2         = parse("trash-2")
	TriangleAlert  = parse("triangle-alert")
	Upload         = parse("upload")
	WifiOff        = parse("wifi-off")
	X              = parse("x")
)
