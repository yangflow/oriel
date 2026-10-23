// Package locale maps a system locale to one of the UI languages every copy catalog carries.
package locale

import (
	"strconv"
	"strings"
)

// Languages are the keys of every copy.json; English is the fallback.
var Languages = []string{"en", "zh-Hans"}

// Language picks the UI language for a system locale such as "zh_TW" or "fr-CA".
// Every Chinese locale, Traditional included, reads Simplified Chinese; everything else reads English.
func Language(locale string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return "zh-Hans"
	}
	return "en"
}

// Resolve picks the UI language: the one chosen in the settings when it is one of Languages, else the language of
// the system locale.
func Resolve(chosen, systemLocale string) string {
	for _, l := range Languages {
		if chosen == l {
			return l
		}
	}
	return Language(systemLocale)
}

// Count fills the count called name into text. {name} becomes the number, and {name:one|other} becomes the first
// form when n is 1 and the second otherwise, as in "{n} {n:entry|entries}". Chinese strings carry no such form.
func Count(text, name string, n int) string {
	num := strconv.Itoa(n)
	for {
		start := strings.Index(text, "{"+name+":")
		if start < 0 {
			break
		}
		end := strings.Index(text[start:], "}")
		if end < 0 {
			break
		}
		end += start
		one, other, _ := strings.Cut(text[start+len(name)+2:end], "|")
		form := other
		if n == 1 {
			form = one
		}
		text = text[:start] + form + text[end+1:]
	}
	return strings.ReplaceAll(text, "{"+name+"}", num)
}
