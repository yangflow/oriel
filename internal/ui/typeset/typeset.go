// Package typeset works out the spacing of Chinese text by the rules of docs/design.md, Chinese typesetting: a gap
// between a Han character and a Latin letter or digit, and tighter runs of full-width punctuation. It only splits text
// into pieces that carry extra letter spacing; no character is inserted or removed, and the pieces joined give back
// the text.
package typeset

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// The spacing of the rules, in the em of the text they are drawn on.
const (
	Gap      = 0.25 // after the character before a boundary of Han and Latin text
	Compress = -0.5 // after the first mark of a pair of full-width punctuation that leaves half an em blank
)

// Run is a stretch of text in one style. Code ends every run of characters: it is never spaced and nothing on its two
// sides is paired. Any other style (a link, emphasis) does not interrupt a run.
type Run struct {
	Text string
	Code bool
}

// Piece is a part of a run. A piece with Em is one character, with that much extra letter spacing after it, in the em
// of its text: letter spacing in the toolkit applies to every character of a text, so such a character stands alone.
type Piece struct {
	Text string
	Em   float32
}

// Text is Layout for one run of prose; it returns nil when no spacing is needed.
func Text(text string) []Piece {
	if out := Layout([]Run{{Text: text}}); out != nil {
		return out[0]
	}
	return nil
}

// Layout splits each run into pieces, the character that needs spacing of the rules in a piece of its own. The result has one
// entry per run, the pieces of that run, or is nil when no run needs spacing, so that the common text costs nothing.
// An entry of the result is empty only for an empty run.
func Layout(runs []Run) [][]Piece {
	var out [][]Piece
	for k, run := range runs {
		if run.Code {
			continue
		}
		var pieces []Piece
		start := 0
		for i, r := range run.Text {
			end := i + utf8.RuneLen(r)
			next := nextRune(runs, k, end)
			if em := spacing(r, next); em != 0 {
				if out == nil {
					out = make([][]Piece, len(runs))
				}
				if start < i {
					pieces = append(pieces, Piece{Text: run.Text[start:i]})
				}
				pieces = append(pieces, Piece{run.Text[i:end], em})
				start = end
			}
		}
		if pieces != nil && start < len(run.Text) {
			pieces = append(pieces, Piece{Text: run.Text[start:]})
		}
		if pieces != nil {
			out[k] = pieces
		}
	}
	for k, pieces := range out {
		if pieces == nil && runs[k].Text != "" {
			out[k] = []Piece{{Text: runs[k].Text}}
		}
	}
	return out
}

// nextRune is the character after offset in run k, in the next run that has text when the run ends; 0 when code
// comes first or there is nothing more.
func nextRune(runs []Run, k, offset int) rune {
	text := runs[k].Text
	for {
		if offset < len(text) {
			r, _ := utf8.DecodeRuneInString(text[offset:])
			return r
		}
		for k++; k < len(runs) && runs[k].Text == "" && !runs[k].Code; k++ {
		}
		if k >= len(runs) || runs[k].Code {
			return 0
		}
		text, offset = runs[k].Text, 0
	}
}

// spacing is the extra letter spacing after a, when b is the character after it.
func spacing(a, b rune) float32 {
	switch {
	case b == 0 || unicode.IsSpace(a) || unicode.IsSpace(b):
		return 0
	case han(a) && latin(b), latin(a) && han(b):
		return Gap
	case closing(a) && (closing(b) || opening(b)), opening(a) && opening(b):
		return Compress
	}
	return 0
}

func han(r rune) bool {
	return r >= 0x3400 && r <= 0x4DBF || r >= 0x4E00 && r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0x20000 && r <= 0x3FFFF
}

// latin is a Latin letter, accented ones too, or a digit 0 to 9.
func latin(r rune) bool {
	return r >= '0' && r <= '9' || unicode.IsLetter(r) && unicode.Is(unicode.Latin, r)
}

func closing(r rune) bool { return strings.ContainsRune("，。、；：！？）」』》〉】", r) }
func opening(r rune) bool { return strings.ContainsRune("（「『《〈【", r) }
