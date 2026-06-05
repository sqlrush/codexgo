package tools

// Snowball / Porter2 English stemmer, mirroring the rust-stemmers v1.2.0
// `Algorithm::English` used by the bm25 v2.3.2 DefaultTokenizer. rust-stemmers
// is a generated port of the canonical Snowball English ("Porter2") algorithm
// (https://snowballstem.org/algorithms/english/stemmer.html). This is a faithful
// hand-port of that algorithm so the tool_search BM25 ranking matches codex
// byte-for-byte.
//
// The algorithm operates on lowercased ASCII words (the tokenizer lowercases and
// normalizes before stemming), so this implementation works on []rune of a
// lowercase string.

import "strings"

// englishStem returns the Snowball English stem of a lowercase word. Mirrors
// rust-stemmers `Algorithm::English`.
func englishStem(word string) string {
	if len(word) <= 2 {
		return word
	}

	s := &stemWord{r: []rune(word)}

	// Step 0 of the Snowball driver: handle the leading apostrophe exceptions
	// and the y-consonant marking happen below. First, the special exceptions.
	if stem, ok := englishExceptions1[word]; ok {
		return stem
	}

	s.removeInitialApostrophe()
	s.markYAsConsonant()
	r1, r2 := s.computeR1R2()

	s.step0()
	s.step1a()

	// Exception set 2: invariant forms checked after step 1a.
	if englishExceptions2[string(s.r)] {
		return string(s.r)
	}

	s.step1b(r1)
	s.step1c()
	s.step2(r1)
	s.step3(r1, r2)
	s.step4(r2)
	s.step5(r1, r2)

	// Restore Y placeholders to lowercase y.
	return strings.ReplaceAll(string(s.r), "Y", "y")
}

// englishExceptions1 are words returned verbatim before the main algorithm.
// Mirrors the Snowball English `exception1` set.
var englishExceptions1 = map[string]string{
	"skis":   "ski",
	"skies":  "sky",
	"dying":  "die",
	"lying":  "lie",
	"tying":  "tie",
	"idly":   "idl",
	"gently": "gentl",
	"ugly":   "ugli",
	"early":  "earli",
	"only":   "onli",
	"singly": "singl",
	"sky":    "sky",
	"news":   "news",
	"howe":   "howe",
	"atlas":  "atlas",
	"cosmos": "cosmos",
	"bias":   "bias",
	"andes":  "andes",
}

// englishExceptions2 are words left invariant after step 1a. Mirrors the
// Snowball English `exception2` set.
var englishExceptions2 = map[string]bool{
	"inning": true, "outing": true, "canning": true, "herring": true,
	"earring": true, "proceed": true, "exceed": true, "succeed": true,
}

// stemWord carries the mutable rune buffer for the stemmer.
type stemWord struct {
	r []rune
}

func isVowel(c rune) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

// removeInitialApostrophe drops a leading apostrophe, mirroring the Snowball
// driver's leading-quote handling.
func (s *stemWord) removeInitialApostrophe() {
	if len(s.r) > 0 && (s.r[0] == '\'' || s.r[0] == '’') {
		s.r = s.r[1:]
	}
}

// markYAsConsonant uppercases y when it acts as a consonant (word-initial, or
// preceded by a vowel). Mirrors Snowball's Y marking.
func (s *stemWord) markYAsConsonant() {
	for i := range s.r {
		if s.r[i] != 'y' {
			continue
		}
		if i == 0 || isVowel(s.r[i-1]) {
			s.r[i] = 'Y'
		}
	}
}

// computeR1R2 returns the R1 and R2 region start offsets. Mirrors Snowball's
// R1/R2 with the English special-prefix rule for gener/commun/arsen.
func (s *stemWord) computeR1R2() (int, int) {
	word := string(s.r)
	r1 := len(s.r)
	switch {
	case strings.HasPrefix(word, "gener"), strings.HasPrefix(word, "arsen"):
		r1 = 5
	case strings.HasPrefix(word, "commun"):
		r1 = 6
	default:
		r1 = regionAfter(s.r, 0)
	}
	r2 := regionAfter(s.r, r1)
	return r1, r2
}

// regionAfter returns the offset of the first non-vowel after a vowel, starting
// the scan at `start`. Mirrors Snowball's region definition.
func regionAfter(r []rune, start int) int {
	i := start
	// find first vowel
	for i < len(r) && !isVowel(r[i]) {
		i++
	}
	// find first non-vowel after that vowel
	for i < len(r) && isVowel(r[i]) {
		i++
	}
	if i < len(r) {
		return i + 1
	}
	return len(r)
}

func (s *stemWord) endsWith(suffix string) bool {
	return strings.HasSuffix(string(s.r), suffix)
}

func (s *stemWord) replaceSuffix(suffix, repl string) {
	// Operate entirely in the rune domain: every Snowball suffix is ASCII (one
	// byte == one rune), but the word may carry multi-byte runes, and a byte
	// slice could cut one in half.
	s.r = append(s.r[:len(s.r)-len([]rune(suffix))], []rune(repl)...)
}

func (s *stemWord) deleteSuffix(n int) {
	s.r = s.r[:len(s.r)-n]
}

// step0 removes trailing apostrophe-s forms. Mirrors Snowball Step 0.
func (s *stemWord) step0() {
	for _, suf := range []string{"'s'", "'s", "'"} {
		if s.endsWith(suf) {
			s.deleteSuffix(len(suf))
			return
		}
	}
}

// step1a handles plural and possessive suffixes. Mirrors Snowball Step 1a.
func (s *stemWord) step1a() {
	w := string(s.r)
	switch {
	case strings.HasSuffix(w, "sses"):
		s.replaceSuffix("sses", "ss")
	case strings.HasSuffix(w, "ied") || strings.HasSuffix(w, "ies"):
		// Replace by i if preceded by more than one letter, otherwise by ie.
		// The matched suffix is 3 ASCII runes ("ied"/"ies"), so slice runes.
		suffix := string(s.r[len(s.r)-3:])
		if len(s.r) > 4 {
			s.replaceSuffix(suffix, "i")
		} else {
			s.replaceSuffix(suffix, "ie")
		}
	case strings.HasSuffix(w, "us") || strings.HasSuffix(w, "ss"):
		// leave unchanged
	case strings.HasSuffix(w, "s"):
		// delete if the preceding word part contains a vowel not immediately
		// before the s (i.e. a vowel before the penultimate char)
		if containsVowelBeforePenultimate(s.r) {
			s.deleteSuffix(1)
		}
	}
}

// containsVowelBeforePenultimate reports whether there is a vowel in the rune
// slice before the last two characters. Mirrors the Step 1a 's' condition.
func containsVowelBeforePenultimate(r []rune) bool {
	if len(r) < 2 {
		return false
	}
	for i := 0; i < len(r)-2; i++ {
		if isVowel(r[i]) {
			return true
		}
	}
	return false
}

// step1b handles -ed/-ing family suffixes. Mirrors Snowball Step 1b.
func (s *stemWord) step1b(r1 int) {
	w := string(s.r)
	// eed / eedly: replace by ee if in R1.
	for _, suf := range []string{"eedly", "eed"} {
		if strings.HasSuffix(w, suf) {
			if len(s.r)-len(suf) >= r1 {
				s.replaceSuffix(suf, "ee")
			}
			return
		}
	}
	// ed / edly / ing / ingly: delete if the preceding word part contains a vowel.
	for _, suf := range []string{"ingly", "edly", "ing", "ed"} {
		if strings.HasSuffix(w, suf) {
			stemPart := s.r[:len(s.r)-len(suf)]
			if !containsVowel(stemPart) {
				return
			}
			s.r = stemPart
			s.step1bPostProcess(r1)
			return
		}
	}
}

func containsVowel(r []rune) bool {
	for _, c := range r {
		if isVowel(c) {
			return true
		}
	}
	return false
}

// step1bPostProcess applies the after-deletion rules of Step 1b.
func (s *stemWord) step1bPostProcess(r1 int) {
	w := string(s.r)
	switch {
	case strings.HasSuffix(w, "at"), strings.HasSuffix(w, "bl"), strings.HasSuffix(w, "iz"):
		s.r = append(s.r, 'e')
	case endsWithDouble(s.r):
		s.deleteSuffix(1)
	case isShortWord(s.r, r1):
		s.r = append(s.r, 'e')
	}
}

// doubleSuffixes are the doubled-consonant endings recognized by Step 1b.
var doubleSuffixes = []string{"bb", "dd", "ff", "gg", "mm", "nn", "pp", "rr", "tt"}

func endsWithDouble(r []rune) bool {
	w := string(r)
	for _, d := range doubleSuffixes {
		if strings.HasSuffix(w, d) {
			return true
		}
	}
	return false
}

// isShortSyllable reports whether the word ends in a short syllable. Mirrors the
// Snowball English short-syllable definition.
func endsWithShortSyllable(r []rune) bool {
	n := len(r)
	if n == 2 {
		// (vowel)(non-vowel) at the start of the word
		return isVowel(r[0]) && !isVowel(r[1])
	}
	if n >= 3 {
		// (non-vowel)(vowel)(non-vowel other than w, x, Y)
		c1, c2, c3 := r[n-3], r[n-2], r[n-1]
		if !isVowel(c1) && isVowel(c2) && !isVowel(c3) {
			switch c3 {
			case 'w', 'x', 'Y':
				return false
			}
			return true
		}
	}
	return false
}

// isShortWord reports whether the word is "short": R1 is the whole word and it
// ends in a short syllable. Mirrors Snowball's short-word definition.
func isShortWord(r []rune, r1 int) bool {
	return r1 >= len(r) && endsWithShortSyllable(r)
}

// step1c replaces a final y/Y by i when preceded by a non-vowel that is itself
// not at the start of the word (Snowball's `non-v not atlimit`). So "dy" stays
// "dy" but "happy" -> "happi". Mirrors Snowball Step 1c.
func (s *stemWord) step1c() {
	n := len(s.r)
	if n < 3 {
		return
	}
	last := s.r[n-1]
	if (last == 'y' || last == 'Y') && !isVowel(s.r[n-2]) {
		s.r[n-1] = 'i'
	}
}

// step2pairs are the (suffix, replacement) pairs for Step 2, longest first.
var step2pairs = []struct{ suffix, repl string }{
	{"ization", "ize"}, {"ational", "ate"}, {"fulness", "ful"},
	{"ousness", "ous"}, {"iveness", "ive"}, {"tional", "tion"},
	{"biliti", "ble"}, {"lessli", "less"}, {"entli", "ent"},
	{"ation", "ate"}, {"alism", "al"}, {"aliti", "al"},
	{"ousli", "ous"}, {"iviti", "ive"}, {"fulli", "ful"},
	{"enci", "ence"}, {"anci", "ance"}, {"abli", "able"},
	{"izer", "ize"}, {"ator", "ate"}, {"alli", "al"},
	{"bli", "ble"},
}

// step2 maps derivational suffixes to shorter ones when in R1. Mirrors Snowball
// Step 2.
func (s *stemWord) step2(r1 int) {
	w := string(s.r)
	for _, p := range step2pairs {
		if strings.HasSuffix(w, p.suffix) {
			if len(s.r)-len(p.suffix) >= r1 {
				s.replaceSuffix(p.suffix, p.repl)
			}
			return
		}
	}
	// "logi" -> "log" when preceded by something in R1 (suffix "ogi").
	if strings.HasSuffix(w, "ogi") {
		if len(s.r)-len("ogi") >= r1 && len(s.r) >= 4 && s.r[len(s.r)-4] == 'l' {
			s.replaceSuffix("ogi", "og")
		}
		return
	}
	// "li" -> delete when in R1 and preceded by a valid li-ending.
	if strings.HasSuffix(w, "li") {
		if len(s.r)-2 >= r1 && len(s.r) >= 3 && isLiEnding(s.r[len(s.r)-3]) {
			s.deleteSuffix(2)
		}
	}
}

// isLiEnding reports whether c is a valid li-ending letter. Mirrors Snowball's
// valid-li set.
func isLiEnding(c rune) bool {
	switch c {
	case 'c', 'd', 'e', 'g', 'h', 'k', 'm', 'n', 'r', 't':
		return true
	}
	return false
}

// step3pairs are the (suffix, replacement) pairs for Step 3, longest first.
var step3pairs = []struct{ suffix, repl string }{
	{"ational", "ate"}, {"tional", "tion"}, {"alize", "al"},
	{"icate", "ic"}, {"iciti", "ic"}, {"ical", "ic"},
	{"ful", ""}, {"ness", ""},
}

// step3 applies further suffix mappings when in R1, plus the R2-gated "ative".
// Mirrors Snowball Step 3.
func (s *stemWord) step3(r1, r2 int) {
	w := string(s.r)
	for _, p := range step3pairs {
		if strings.HasSuffix(w, p.suffix) {
			if len(s.r)-len(p.suffix) >= r1 {
				s.replaceSuffix(p.suffix, p.repl)
			}
			return
		}
	}
	if strings.HasSuffix(w, "ative") {
		if len(s.r)-len("ative") >= r2 {
			s.deleteSuffix(len("ative"))
		}
	}
}

// step4suffixes are the Step 4 suffixes (deleted when in R2), longest first.
var step4suffixes = []string{
	"ement", "ance", "ence", "able", "ible", "ment",
	"ant", "ent", "ism", "ate", "iti", "ous", "ive", "ize",
	"al", "er", "ic",
}

// step4 deletes terminal derivational suffixes in R2. Mirrors Snowball Step 4.
func (s *stemWord) step4(r2 int) {
	w := string(s.r)
	for _, suf := range step4suffixes {
		if strings.HasSuffix(w, suf) {
			if len(s.r)-len(suf) >= r2 {
				s.deleteSuffix(len(suf))
			}
			return
		}
	}
	// "ion" -> delete in R2 only when preceded by s or t.
	if strings.HasSuffix(w, "ion") {
		if len(s.r)-3 >= r2 && len(s.r) >= 4 {
			prev := s.r[len(s.r)-4]
			if prev == 's' || prev == 't' {
				s.deleteSuffix(3)
			}
		}
	}
}

// step5 removes a final e or l per the R1/R2 rules. Mirrors Snowball Step 5.
func (s *stemWord) step5(r1, r2 int) {
	n := len(s.r)
	if n == 0 {
		return
	}
	last := s.r[n-1]
	if last == 'e' {
		// delete if in R2, or in R1 and not preceded by a short syllable
		if n-1 >= r2 {
			s.deleteSuffix(1)
			return
		}
		if n-1 >= r1 && !endsWithShortSyllable(s.r[:n-1]) {
			s.deleteSuffix(1)
		}
		return
	}
	if last == 'l' {
		// delete if in R2 and preceded by l
		if n-1 >= r2 && n >= 2 && s.r[n-2] == 'l' {
			s.deleteSuffix(1)
		}
	}
}
