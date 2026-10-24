package reader

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/yangflow/oriel/internal/client"
)

// What an article becomes as Markdown, see docs/product.md "阅读器", Export as Markdown. It is made from the same
// blocks the reader draws, so the text matches what is read, and it is one function for both the copy and the file.

// markdown is the article as a Markdown document: the title, the lines of its source, author, date and link, and
// the text of blocks. e gives the metadata, at is where its date is told, and blocks may be empty.
func markdown(e client.Entry, blocks []block, at *time.Location) string {
	var out strings.Builder
	title := firstLine(e.Title)
	if title == "" {
		title = firstLine(e.SourceTitle)
	}
	if title != "" {
		out.WriteString("# " + escape(title, false) + "\n")
	}
	var lines []string
	if v := firstLine(e.SourceTitle); v != "" {
		lines = append(lines, "- Source: "+escape(v, false))
	}
	if v := firstLine(e.Author); v != "" {
		lines = append(lines, "- Author: "+escape(v, false))
	}
	if ms := published(e); ms != 0 {
		lines = append(lines, "- Date: "+time.UnixMilli(ms).In(at).Format("2006-01-02 15:04"))
	}
	if v := strings.TrimSpace(e.URL); v != "" {
		lines = append(lines, "- Link: <"+strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20").Replace(v)+">")
	}
	if len(lines) > 0 {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(strings.Join(lines, "\n") + "\n")
	}
	if body := render(blocks); body != "" {
		if out.Len() > 0 {
			out.WriteString("\n")
		}
		out.WriteString(body + "\n")
	}
	return out.String()
}

func published(e client.Entry) int64 {
	if e.PublishedAt != 0 {
		return e.PublishedAt
	}
	return e.CreatedAt
}

// firstLine is s with its white space collapsed to single spaces.
func firstLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// render writes blocks as Markdown without the end newline: a blank line between blocks, except between the items of
// a list, which are tight.
func render(blocks []block) string {
	var body, notes []block
	for _, b := range blocks {
		switch {
		case b.kind == embed: // the rows say what the reader cannot show; they are not text
		case b.note > 0:
			notes = append(notes, b)
		case b.notes && b.kind == rule: // the line above the notes, which have their own place
		default:
			body = append(body, b)
		}
	}
	text := renderBlocks(body)
	if defs := definitions(notes); defs != "" {
		if text != "" {
			text += "\n\n"
		}
		text += defs
	}
	return text
}

// definitions writes the footnotes as [^n]: lines, the later blocks of a note indented under its first.
func definitions(notes []block) string {
	var lines []string
	for i, b := range notes {
		text := renderBlock(b)
		if text == "" {
			continue
		}
		first := i == 0 || notes[i-1].note != b.note
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		for j, line := range strings.Split(text, "\n") {
			switch {
			case first && j == 0:
				line = "[^" + strconv.Itoa(b.note) + "]: " + line
			case line != "":
				line = "    " + line
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func renderBlocks(blocks []block) string {
	var lines []string
	for i, b := range blocks {
		text := renderBlock(b)
		if text == "" {
			continue
		}
		prefix := func(depth, quote int, first string) []string {
			var out []string
			for j, line := range strings.Split(text, "\n") {
				lead := strings.Repeat("    ", max(depth-1, 0))
				switch {
				case j == 0 && first != "":
					lead += bullet(first) + " "
				case depth > 0:
					lead += "    "
				}
				out = append(out, quoted(quote, lead+line))
			}
			return out
		}
		if i > 0 && len(lines) > 0 {
			prev := blocks[i-1]
			if !(b.marker != "" && prev.marker != "" && prev.kind == paragraph && prev.quote == b.quote && ordered(prev.marker) == ordered(b.marker)) {
				lines = append(lines, quoted(min(prev.quote, b.quote), ""))
			}
		}
		lines = append(lines, prefix(b.depth, b.quote, b.marker)...)
	}
	return strings.Join(lines, "\n")
}

// ordered tells a numbered marker from a bullet.
func ordered(marker string) bool { return strings.HasSuffix(marker, ".") }

// bullet is the marker of a list item in Markdown: a dash for the bullets, the number as it is.
func bullet(marker string) string {
	if ordered(marker) {
		return marker
	}
	return "-"
}

// quoted puts the quote marks of depth quotes before a line.
func quoted(depth int, line string) string {
	if depth == 0 {
		return line
	}
	mark := strings.Repeat("> ", depth)
	if line == "" {
		return strings.TrimSpace(mark)
	}
	return mark + line
}

// renderBlock is the text of a block, without its marker, quote marks and indent.
func renderBlock(b block) string {
	switch b.kind {
	case heading:
		return strings.Repeat("#", b.level) + " " + inline(b.spans, true)
	case code:
		fence := "```"
		for strings.Contains(b.text, fence) {
			fence += "`"
		}
		return fence + "\n" + b.text + "\n" + fence
	case picture:
		return "![" + strings.NewReplacer("[", `\[`, "]", `\]`, "\n", " ").Replace(b.alt) + "](" + destination(b.src) + ")"
	case caption:
		if text := inline(b.spans, false); text != "" {
			return "*" + strings.ReplaceAll(text, "*", `\*`) + "*"
		}
		return ""
	case rule:
		return "---"
	case table:
		return renderTable(b.table)
	}
	return lineMarks(inline(b.spans, false))
}

// renderTable writes a table as a GFM table: its first row heads it, a cell that covers columns is followed by
// empty ones, and the caption is an italic paragraph below.
func renderTable(g *grid) string {
	if g == nil || len(g.rows) == 0 {
		return ""
	}
	row := func(cells []cell) string {
		var out []string
		for _, c := range cells {
			text := strings.NewReplacer("\\\n", " ", "\n", " ", "|", `\|`).Replace(inline(c.spans, false))
			out = append(out, text)
			for range max(c.span, 1) - 1 {
				out = append(out, "")
			}
		}
		for len(out) < g.cols {
			out = append(out, "")
		}
		return "| " + strings.Join(out[:g.cols], " | ") + " |"
	}
	lines := []string{row(g.rows[0])}
	sep := make([]string, g.cols)
	for i := range sep {
		sep[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(sep, " | ")+" |")
	for _, r := range g.rows[1:] {
		lines = append(lines, row(r))
	}
	if text := inline(g.caption, false); text != "" {
		lines = append(lines, "", "*"+strings.ReplaceAll(text, "*", `\*`)+"*")
	}
	return strings.Join(lines, "\n")
}

// destination is a link address that cannot end the link early.
func destination(address string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(address)
}

// inline writes spans as Markdown. A heading is bold already, so plain says not to mark that.
func inline(spans []span, plain bool) string {
	var out strings.Builder
	for _, sp := range spans {
		if sp.back {
			continue
		}
		if sp.ref > 0 {
			out.WriteString("[^" + strconv.Itoa(sp.ref) + "]")
			continue
		}
		text := sp.text
		if sp.mono {
			text = strings.ReplaceAll(text, "\n", " ")
		}
		lead, core, trail := edges(text)
		var body string
		if sp.mono {
			body = codeSpan(core)
		} else {
			body = escape(core, true)
		}
		if core != "" {
			if sp.gone {
				body = "~~" + body + "~~"
			}
			switch {
			case sp.bold && !plain && sp.italic:
				body = "***" + body + "***"
			case sp.bold && !plain:
				body = "**" + body + "**"
			case sp.italic:
				body = "*" + body + "*"
			}
			if sp.link != "" {
				body = "[" + body + "](" + destination(sp.link) + ")"
			}
		}
		out.WriteString(escape(lead, true) + body + escape(trail, true))
	}
	text := out.String()
	// A break at the end of a block is nothing.
	for strings.HasSuffix(text, "\\\n") || strings.HasSuffix(text, " ") {
		text = strings.TrimSuffix(strings.TrimSuffix(text, "\\\n"), " ")
	}
	return text
}

// edges splits s into the white space before it, its text, and the white space after it, which stays outside of the
// marks around the text, as they do not hold with a space inside.
func edges(s string) (lead, core, trail string) {
	core = strings.TrimLeftFunc(s, unicode.IsSpace)
	lead = s[:len(s)-len(core)]
	trimmed := strings.TrimRightFunc(core, unicode.IsSpace)
	return lead, trimmed, core[len(trimmed):]
}

func codeSpan(s string) string {
	if s == "" {
		return ""
	}
	longest, run := 0, 0
	for _, r := range s {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		s = " " + s + " "
	}
	return fence + s + fence
}

var lineStart = regexp.MustCompile(`^(\s*)(#|\+|-|\d+[.)])`)

// escape puts a backslash before what Markdown would take for marks. In text, a line break is a backslash at the end of
// the line.
func escape(s string, text bool) string {
	var out strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '*', '_', '`', '~', '[', ']', '<', '>':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\n':
			if text {
				out.WriteString("\\\n")
			} else {
				out.WriteByte(' ')
			}
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// lineMarks escapes what would make a line of a paragraph a heading or an item: a #, - or + at its start, or a
// number and its dot.
func lineMarks(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if m := lineStart.FindStringSubmatch(line); m != nil {
			at := len(m[1])
			if len(m[2]) > 1 { // a number: the backslash goes before its dot
				at += len(m[2]) - 1
			}
			lines[i] = line[:at] + `\` + line[at:]
		}
	}
	return strings.Join(lines, "\n")
}

// fileName is the name a saved article is offered under: its title without what file names cannot hold, and .md.
func fileName(title string) string {
	var name strings.Builder
	for _, r := range firstLine(title) {
		switch {
		case strings.ContainsRune(`\/:*?"<>|`, r) || unicode.IsControl(r):
			name.WriteByte('-')
		default:
			name.WriteRune(r)
		}
	}
	s := name.String()
	if utf8.RuneCountInString(s) > 80 {
		s = string([]rune(s)[:80])
	}
	s = strings.Trim(s, " .")
	if s == "" || strings.Trim(s, "-") == "" {
		s = "entry"
	}
	switch strings.ToUpper(strings.SplitN(s, ".", 2)[0]) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		s += "_"
	}
	return s + ".md"
}
