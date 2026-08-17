package tools

// DefaultTokenizer port for the bm25 crate v2.3.2 with Language::English. The
// pipeline mirrors default_tokenizer.rs `_tokenize`:
//
//  1. normalize: deunicode the text to ASCII. The codex search texts are pure
//     ASCII, where deunicode is the identity, so this port treats normalization
//     as a no-op (see DEVIATIONS).
//  2. lowercase.
//  3. split on unicode word boundaries (unicode-segmentation `unicode_words`,
//     i.e. UAX #29 segments that contain at least one alphanumeric rune).
//  4. drop NLTK English stopwords (the stop-words crate's English list).
//  5. stem with the Snowball/Porter2 English stemmer.

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// bm25Tokenize runs the full DefaultTokenizer pipeline for Language::English.
// Mirrors `DefaultTokenizer::_tokenize`. Empty input yields no tokens.
func bm25Tokenize(text string) []string {
	if text == "" {
		return nil
	}
	// (1) normalize (ASCII deunicode is identity here) then (2) lowercase.
	lowered := strings.ToLower(text)

	// (3) split on unicode word boundaries.
	words := unicodeWords(lowered)

	tokens := make([]string, 0, len(words))
	for _, w := range words {
		// (4) drop English stopwords.
		if englishStopwords[w] {
			continue
		}
		// (5) stem.
		tokens = append(tokens, englishStem(w))
	}
	return tokens
}

// unicodeWords splits text into UAX #29 word segments that contain at least one
// alphanumeric rune, mirroring unicode-segmentation's `unicode_words`.
func unicodeWords(text string) []string {
	var words []string
	rest := text
	state := -1
	for len(rest) > 0 {
		var word string
		word, rest, state = uniseg.FirstWordInString(rest, state)
		if hasAlphanumeric(word) {
			words = append(words, word)
		}
	}
	return words
}

// hasAlphanumeric reports whether s contains any letter or digit, the filter
// `unicode_words` applies to keep only word-bearing segments.
func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return true
		}
	}
	return false
}
