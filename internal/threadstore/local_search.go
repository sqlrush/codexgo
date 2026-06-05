package threadstore

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sqlrush/codexgo/internal/protocol"
	"github.com/sqlrush/codexgo/internal/rollout"
)

// This file ports the codex local thread-search behavior. It mirrors:
//   - thread-store/src/local/search_threads.rs::search_threads (orchestration)
//   - rollout/src/search.rs (transcript scan + snippet generation)
//
// codex first finds rollout files whose RAW transcript contains the term
// (ripgrep `--fixed-strings --ignore-case` over the JSON-escaped term, with a
// pure scan fallback), then lists threads in sort order and keeps only those
// whose rollout path matched AND that yield a conversation-text snippet around
// the match. This is the "ripgrep transcript scan" the spec-19 row tracked as
// missing; the port reproduces the dependency-free `scan_rollout_paths` /
// `rollout_contains` fallback path, which is byte-equivalent to the ripgrep path
// for the case-insensitive fixed-string semantics codex uses.

// matchContextBeforeChars / matchContextAfterChars bound the snippet excerpt,
// mirroring MATCH_CONTEXT_BEFORE_CHARS / MATCH_CONTEXT_AFTER_CHARS in
// rollout/src/search.rs.
const (
	matchContextBeforeChars = 48
	matchContextAfterChars  = 96
)

// userMessageBegin mirrors codex_protocol::protocol::USER_MESSAGE_BEGIN, the
// marker stripped from a stored user message before snippet extraction.
const userMessageBegin = "## My request for Codex:"

// searchThreads searches stored threads by scanning the rollout transcripts,
// mirroring thread-store/src/local/search_threads.rs::search_threads. It returns
// up to params.PageSize results in sort order, each carrying a snippet excerpted
// around the first conversation-text match, plus a next cursor when more matches
// remain.
func (s *LocalThreadStore) searchThreads(ctx context.Context, params SearchThreadsParams) (ThreadSearchPage, error) {
	term := params.SearchTerm
	if term == "" {
		return ThreadSearchPage{}, invalidRequestError("thread/search requires search_term")
	}

	// Validate the cursor up front so a bad cursor errors before any scanning,
	// matching the Rust parse_cursor check.
	if params.Cursor != nil {
		if _, ok := decodeCursor(*params.Cursor); !ok {
			return ThreadSearchPage{}, invalidRequestError("invalid cursor: %s", *params.Cursor)
		}
	}

	// Phase 1: raw-transcript path match (mirrors search_rollout_paths).
	matchingPaths, err := searchRolloutPaths(s.config.CodexHome, params.Archived, term)
	if err != nil {
		return ThreadSearchPage{}, internalError(err, "failed to search rollout contents")
	}
	if len(matchingPaths) == 0 {
		return ThreadSearchPage{}, nil
	}

	pageSize := normalizePageSize(params.PageSize)
	// codex widens the per-page scan window to params.page_size * 8, clamped to
	// [256, 2048], so it can collect enough matching candidates per list page.
	scanSize := clampInt(pageSize*8, 256, listScanFallbackPageMax)

	// remaining tracks which matched paths still need a thread, mirroring the
	// `remaining_paths` HashSet that is drained as list pages are consumed.
	remaining := make(map[string]bool, len(matchingPaths))
	for path := range matchingPaths {
		remaining[path] = true
	}

	results := make([]StoredThreadSearchResult, 0, pageSize+1)
	pageCursor := params.Cursor

	// Phase 2: list threads in sort order and keep those whose path matched and
	// that yield a snippet, accumulating up to pageSize+1 to detect overflow.
	for {
		listParams := ListThreadsParams{
			PageSize:       scanSize,
			Cursor:         pageCursor,
			SortKey:        params.SortKey,
			SortDirection:  params.SortDirection,
			AllowedSources: params.AllowedSources,
			Archived:       params.Archived,
		}
		page, listErr := s.listThreads(ctx, listParams)
		if listErr != nil {
			return ThreadSearchPage{}, listErr
		}

		for i := range page.Items {
			thread := page.Items[i]
			if thread.RolloutPath == nil {
				continue
			}
			path := *thread.RolloutPath
			if !remaining[path] {
				continue
			}
			delete(remaining, path)

			snippet, ok, snerr := firstRolloutContentMatchSnippet(path, term)
			if snerr != nil {
				return ThreadSearchPage{}, internalError(snerr, "failed to read rollout search match")
			}
			if !ok {
				continue
			}
			results = append(results, StoredThreadSearchResult{Thread: thread, Snippet: snippet})
			if len(results) > pageSize {
				break
			}
		}

		pageCursor = page.NextCursor
		if len(results) > pageSize || len(remaining) == 0 || pageCursor == nil {
			break
		}
	}

	// Overflow detection: a (pageSize+1)th result means there is a next page.
	moreAvailable := len(results) > pageSize
	if moreAvailable {
		results = results[:pageSize]
	}
	var nextCursor *string
	if moreAvailable && len(results) > 0 {
		last := results[len(results)-1].Thread
		cursor := cursorFromStoredThread(last, params.SortKey)
		nextCursor = &cursor
	}

	return ThreadSearchPage{Items: results, NextCursor: nextCursor}, nil
}

// searchRolloutPaths returns the set of rollout file paths under the active or
// archived sessions tree whose RAW transcript contains the term
// (case-insensitive, literal), mirroring rollout/src/search.rs::search_rollout_paths
// + scan_rollout_paths. The match is performed on the JSON-escaped term so it
// matches the escaped form stored inside the JSONL string values, exactly as
// ripgrep `--fixed-strings` does against the on-disk bytes.
func searchRolloutPaths(codexHome string, archived bool, term string) (map[string]bool, error) {
	subdir := rollout.SessionsSubdir
	if archived {
		subdir = rollout.ArchivedSessionsSubdir
	}
	root := filepath.Join(codexHome, subdir)

	matcher, err := caseInsensitiveLiteralRegex(jsonEscapedSearchTerm(term))
	if err != nil {
		return nil, err
	}

	matches := make(map[string]bool)
	if _, statErr := os.Stat(root); statErr != nil {
		if os.IsNotExist(statErr) {
			return matches, nil
		}
		return nil, statErr
	}

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".jsonl" {
			return nil
		}
		ok, containsErr := rolloutContains(path, matcher)
		if containsErr != nil {
			return containsErr
		}
		if ok {
			matches[path] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return matches, nil
}

// rolloutContains reports whether any line of the file at path matches matcher,
// mirroring rollout/src/search.rs::rollout_contains (a per-line scan).
func rolloutContains(path string, matcher *regexp.Regexp) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRolloutLineBytes)
	for scanner.Scan() {
		if matcher.MatchString(scanner.Text()) {
			return true, nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return false, scanErr
	}
	return false, nil
}

// maxRolloutLineBytes bounds a single rollout JSONL line read; rollout lines are
// individual items so this is generous.
const maxRolloutLineBytes = 16 * 1024 * 1024

// firstRolloutContentMatchSnippet scans the rollout at path for the first line
// whose JSON-escaped raw form matches term AND whose conversation text yields an
// excerpt around the (unescaped) term, mirroring
// rollout/src/search.rs::first_rollout_content_match_snippet. The bool is false
// when no conversation-text line matches.
func firstRolloutContentMatchSnippet(path, term string) (string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer file.Close()

	jsonMatcher, err := caseInsensitiveLiteralRegex(jsonEscapedSearchTerm(term))
	if err != nil {
		return "", false, err
	}
	textMatcher, err := caseInsensitiveLiteralRegex(term)
	if err != nil {
		return "", false, err
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRolloutLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if !jsonMatcher.MatchString(line) {
			continue
		}
		if snippet, ok := contentMatchSnippet(line, textMatcher); ok {
			return snippet, true, nil
		}
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return "", false, scanErr
	}
	return "", false, nil
}

// contentMatchSnippet decodes a rollout JSONL line, extracts its conversation
// text, and excerpts around the match, mirroring
// rollout/src/search.rs::content_match_snippet. The bool is false when the line
// is not a conversation item or has no match in its text.
func contentMatchSnippet(jsonlLine string, matcher *regexp.Regexp) (string, bool) {
	var rolloutLine rollout.RolloutLine
	if err := json.Unmarshal([]byte(strings.TrimSpace(jsonlLine)), &rolloutLine); err != nil {
		return "", false
	}
	text, ok := conversationTextFromItem(rolloutLine.Item)
	if !ok {
		return "", false
	}
	return excerptAroundMatch(text, matcher)
}

// conversationTextFromItem returns the searchable conversation text for a
// rollout item, mirroring rollout/src/search.rs::conversation_text_from_item.
// Only user/agent event messages and user/assistant response Message items
// carry conversation text; every other item kind is skipped.
func conversationTextFromItem(item rollout.RolloutItem) (string, bool) {
	switch item.Kind {
	case rollout.RolloutItemKindEventMsg:
		return conversationTextFromEvent(item.EventMsg)
	case rollout.RolloutItemKindResponseItem:
		return conversationTextFromResponseItem(item.ResponseItem)
	default:
		return "", false
	}
}

// conversationTextFromEvent extracts conversation text from a user/agent event
// message, mirroring the EventMsg arms of conversation_text_from_item.
func conversationTextFromEvent(event *protocol.EventMsg) (string, bool) {
	if event == nil {
		return "", false
	}
	switch event.Type {
	case protocol.EventMsgKindUserMessage:
		if event.UserMessage == nil {
			return "", false
		}
		text := stripUserMessagePrefix(event.UserMessage.Message)
		if text == "" {
			return "", false
		}
		return text, true
	case protocol.EventMsgKindAgentMessage:
		if event.AgentMessage == nil {
			return "", false
		}
		text := strings.TrimSpace(event.AgentMessage.Message)
		if text == "" {
			return "", false
		}
		return text, true
	default:
		return "", false
	}
}

// conversationTextFromResponseItem extracts conversation text from a user- or
// assistant-role response Message, mirroring the ResponseItem::Message arm of
// conversation_text_from_item: the input/output text parts joined by a space.
func conversationTextFromResponseItem(item *protocol.ResponseItem) (string, bool) {
	if item == nil || item.Type != protocol.ResponseItemKindMessage {
		return "", false
	}
	parts := make([]string, 0, len(item.Content))
	for i := range item.Content {
		if text, ok := contentItemText(item.Content[i]); ok {
			parts = append(parts, text)
		}
	}
	text := strings.Join(parts, " ")
	if strings.TrimSpace(text) == "" || (item.Role != "user" && item.Role != "assistant") {
		return "", false
	}
	return text, true
}

// contentItemText returns the text of an input/output text content part,
// mirroring rollout/src/search.rs::content_item_text (images carry no text).
func contentItemText(item protocol.ContentItem) (string, bool) {
	switch item.Type {
	case protocol.ContentItemKindInputText, protocol.ContentItemKindOutputText:
		return item.Text, true
	default:
		return "", false
	}
}

// stripUserMessagePrefix removes the USER_MESSAGE_BEGIN marker (and everything
// before it) from a stored user message, then trims, mirroring
// rollout/src/search.rs::strip_user_message_prefix.
func stripUserMessagePrefix(text string) string {
	if idx := strings.Index(text, userMessageBegin); idx >= 0 {
		return strings.TrimSpace(text[idx+len(userMessageBegin):])
	}
	return strings.TrimSpace(text)
}

// excerptAroundMatch normalizes whitespace, finds the first match, and builds a
// trimmed excerpt with leading/trailing ellipsis markers, mirroring
// rollout/src/search.rs::excerpt_around_match. The bool is false when there is
// no match or the excerpt is empty.
func excerptAroundMatch(text string, matcher *regexp.Regexp) (string, bool) {
	normalized := normalizePreviewText(text)
	loc := matcher.FindStringIndex(normalized)
	if loc == nil {
		return "", false
	}
	matchStart, matchEnd := loc[0], loc[1]
	excerptStart := charStartBefore(normalized, matchStart, matchContextBeforeChars)
	excerptEnd := charEndAfter(normalized, matchEnd, matchContextAfterChars)
	excerpt := strings.TrimSpace(normalized[excerptStart:excerptEnd])
	if excerpt == "" {
		return "", false
	}

	var snippet strings.Builder
	if excerptStart > 0 {
		snippet.WriteString("... ")
	}
	snippet.WriteString(excerpt)
	if excerptEnd < len(normalized) {
		snippet.WriteString(" ...")
	}
	return snippet.String(), true
}

// normalizePreviewText collapses any run of whitespace to a single space,
// mirroring rollout/src/search.rs::normalize_preview_text
// (`text.split_whitespace().join(" ")`).
func normalizePreviewText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// charStartBefore returns the byte index charsBefore characters before
// byteIndex, mirroring rollout/src/search.rs::char_start_before. It walks
// runes backward from byteIndex.
func charStartBefore(text string, byteIndex, charsBefore int) int {
	prefix := text[:byteIndex]
	// Collect rune start byte-offsets in prefix.
	offsets := make([]int, 0, len(prefix))
	for off := range prefix {
		offsets = append(offsets, off)
	}
	// nth(charsBefore) over the reversed iterator: index from the end.
	idxFromEnd := charsBefore
	if idxFromEnd >= len(offsets) {
		return 0
	}
	return offsets[len(offsets)-1-idxFromEnd]
}

// charEndAfter returns the byte index charsAfter characters after byteIndex,
// mirroring rollout/src/search.rs::char_end_after. It walks runes forward from
// byteIndex.
func charEndAfter(text string, byteIndex, charsAfter int) int {
	suffix := text[byteIndex:]
	count := 0
	for off := range suffix {
		if count == charsAfter {
			return byteIndex + off
		}
		count++
	}
	return len(text)
}

// jsonEscapedSearchTerm returns the term as it would appear inside a JSON string
// (without the surrounding quotes), mirroring
// rollout/src/search.rs::json_escaped_search_term. This lets a fixed-string scan
// match the escaped form stored in the JSONL string values.
func jsonEscapedSearchTerm(term string) string {
	encoded, err := json.Marshal(term)
	if err != nil {
		return term
	}
	// Strip the surrounding quotes, matching serialized[1..len-1].
	if len(encoded) >= 2 {
		return string(encoded[1 : len(encoded)-1])
	}
	return term
}

// caseInsensitiveLiteralRegex builds a case-insensitive regex that matches term
// literally (escaping any regex metacharacters), mirroring
// rollout/src/search.rs::case_insensitive_literal_regex.
func caseInsensitiveLiteralRegex(term string) (*regexp.Regexp, error) {
	return regexp.Compile("(?i)" + regexp.QuoteMeta(term))
}
