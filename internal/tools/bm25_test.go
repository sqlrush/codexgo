package tools

import "testing"

// The five multi-agent search texts, in the registration order codex uses
// (spawn, send_input, resume, wait, close — the spec_plan runtime order).
var multiAgentSearchTexts = []string{
	// spawn_agent
	"spawn_agent spawn agent subagent sub-agent delegate delegation parallel work worker explorer no-apps fork model reasoning",
	// send_input
	"send_input send message existing agent subagent follow up interrupt redirect queue target",
	// resume_agent
	"resume_agent resume reopen closed agent subagent thread id target",
	// wait_agent
	"wait_agent wait agent subagent status final result complete timeout targets",
	// close_agent
	"close_agent close shutdown stop agent subagent thread status target",
}

// TestBM25SearchSpawnAgentsOrder asserts the BM25 ranking for the query
// "spawn agents" over the five multi-agent search texts matches codex: the hit
// order must be spawn_agent, close_agent, resume_agent, wait_agent, send_input
// (entry indices 0, 4, 2, 3, 1 in registration order).
func TestBM25SearchSpawnAgentsOrder(t *testing.T) {
	docs := make([]bm25Document, len(multiAgentSearchTexts))
	for i, txt := range multiAgentSearchTexts {
		docs[i] = bm25Document{id: i, contents: txt}
	}
	engine := newBM25SearchEngine(docs)

	results := engine.search("spawn agents", ToolSearchDefaultLimit)

	wantOrder := []int{0, 4, 2, 3, 1} // spawn, close, resume, wait, send_input
	if len(results) != len(wantOrder) {
		t.Fatalf("got %d results, want %d: %+v", len(results), len(wantOrder), results)
	}
	for i, want := range wantOrder {
		if results[i].id != want {
			t.Errorf("result[%d].id = %d, want %d (full: %+v)", i, results[i].id, want, results)
		}
	}
	// Scores must be strictly descending for this query (no ties), which is what
	// makes the order deterministic.
	for i := 1; i < len(results); i++ {
		if results[i-1].score < results[i].score {
			t.Errorf("scores not descending at %d: %v", i, results)
		}
	}
}

// TestBM25TokenizeSpawnText spot-checks the tokenizer: UAX #29 word
// segmentation keeps "spawn_agent" as a single connected word (underscore is an
// ExtendNumLet connector), splits "sub-agent" on the hyphen, drops the NLTK
// stopword "no", and stems each surviving token (apps -> app).
func TestBM25TokenizeSpawnText(t *testing.T) {
	got := bm25Tokenize("spawn_agent sub-agent no-apps")
	want := []string{"spawn_ag", "sub", "agent", "app"}
	if len(got) != len(want) {
		t.Fatalf("tokens = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestEnglishStopwordListSize guards the NLTK English stopword list size (179),
// the count shipped by the stop-words crate v0.9.0.
func TestEnglishStopwordListSize(t *testing.T) {
	if len(englishStopwords) != englishStopwordCount {
		t.Errorf("english stopword count = %d, want %d", len(englishStopwords), englishStopwordCount)
	}
}
