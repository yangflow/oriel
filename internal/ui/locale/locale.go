// Package locale maps a system locale to one of the UI languages every copy catalog carries.
package locale

import "strings"

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
