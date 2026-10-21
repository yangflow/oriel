package fetch

import (
	"strings"
	"unicode"

	"github.com/abadojack/whatlanggo"
)

const (
	// languageSample is how many runes of an entry the detector reads; more only costs time.
	languageSample = 3000
	// languageMinLetters is how many letters the sample needs; a shorter text says too little about its language.
	languageMinLetters = 20
)

// traditional and simplified list common characters that differ between the two scripts, in the same order, so
// that the nth rune of one is the counterpart of the nth rune of the other.
const (
	traditional = "這們個說時會來對國學發還過經點與為後開關長從東應業產見買車專區書讀頭體實義電話門間動認種師無聽習機歲島鄉樂歷"
	simplified  = "这们个说时会来对国学发还过经点与为后开关长从东应业产见买车专区书读头体实义电话门间动认种师无听习机岁岛乡乐历"
)

var (
	traditionalOnly = runeSet(traditional, simplified)
	simplifiedOnly  = runeSet(simplified, traditional)
)

// runeSet returns the runes of in that are not also in other.
func runeSet(in, other string) map[rune]bool {
	skip := map[rune]bool{}
	for _, r := range other {
		skip[r] = true
	}
	set := map[rune]bool{}
	for _, r := range in {
		if !skip[r] {
			set[r] = true
		}
	}
	return set
}

// iso6391 maps the ISO 639-3 codes of the detector to ISO 639-1; a language without a two-letter code keeps
// its three-letter one, which is a valid BCP 47 tag as well.
var iso6391 = map[string]string{
	"afr": "af", "aka": "ak", "amh": "am", "arb": "ar", "azj": "az", "bel": "be", "ben": "bn",
	"bul": "bg", "ces": "cs", "dan": "da", "deu": "de", "ell": "el", "eng": "en", "epo": "eo", "est": "et",
	"fin": "fi", "fra": "fr", "guj": "gu", "hat": "ht", "hau": "ha", "heb": "he", "hin": "hi", "hrv": "hr",
	"hun": "hu", "ibo": "ig", "ind": "id", "ita": "it", "jav": "jv", "jpn": "ja", "kan": "kn", "kat": "ka",
	"khm": "km", "kin": "rw", "kor": "ko", "kur": "ku", "lav": "lv", "lit": "lt", "mal": "ml", "mar": "mr",
	"mkd": "mk", "mlg": "mg", "mya": "my", "nep": "ne", "nld": "nl", "nno": "nn", "nob": "nb", "nya": "ny",
	"ori": "or", "orm": "om", "pan": "pa", "pes": "fa", "pol": "pl", "por": "pt", "ron": "ro", "run": "rn",
	"rus": "ru", "sin": "si", "slv": "sl", "sna": "sn", "som": "so", "spa": "es", "srp": "sr", "swe": "sv",
	"tam": "ta", "tel": "te", "tgl": "tl", "tha": "th", "tir": "ti", "tuk": "tk", "tur": "tr", "uig": "ug",
	"ukr": "uk", "urd": "ur", "uzb": "uz", "vie": "vi", "ydd": "yi", "yor": "yo", "zul": "zu",
}

// detectLanguage returns the BCP 47 code of the language of text: the ISO 639-1 code, "zh-Hans" or "zh-Hant" for
// Chinese. It returns "" when the text is too short or the detector is not sure.
func detectLanguage(text string) string {
	if runes := []rune(text); len(runes) > languageSample {
		text = string(runes[:languageSample])
	}
	letters := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	if letters < languageMinLetters {
		return ""
	}
	info := whatlanggo.Detect(text)
	if info.Script == nil || !info.IsReliable() {
		return ""
	}
	if info.Lang == whatlanggo.Cmn {
		return chineseScript(text)
	}
	code := info.Lang.Iso6393()
	if short, ok := iso6391[code]; ok {
		return short
	}
	return code
}

// chineseScript tells Traditional from Simplified Chinese by counting characters only one of them uses; the tie
// goes to Simplified.
func chineseScript(text string) string {
	hant, hans := 0, 0
	for _, r := range text {
		switch {
		case traditionalOnly[r]:
			hant++
		case simplifiedOnly[r]:
			hans++
		}
	}
	if hant > hans {
		return "zh-Hant"
	}
	return "zh-Hans"
}

// entryLanguage detects the language of an entry from its title and its plain text.
func entryLanguage(title, plain string) string {
	return detectLanguage(strings.TrimSpace(title + "\n" + plain))
}
