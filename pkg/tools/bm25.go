package tools

// BM25 search engine, mirroring the bm25 crate v2.3.2
// (https://docs.rs/bm25/2.3.2) with Language::English, as used by the codex
// ToolSearchHandler. The pipeline is:
//
//   - DefaultTokenizer (default_tokenizer.rs): deunicode-normalize (a no-op for
//     ASCII), lowercase, split on unicode word boundaries, drop NLTK English
//     stopwords, then Snowball/Porter2 English stem (see bm25_stemmer.go).
//   - Embedder (embedder.rs): per-token BM25 saturation value
//     freq*(k1+1) / (freq + k1*(1 - b + b*(doc_len/avgdl))) with k1=1.2, b=0.75
//     and avgdl = mean document token count over the corpus.
//   - Scorer (scorer.rs): score = Σ over the query's token indices of
//     idf(token) * doc_token_value, with idf = ln(1 + (N - df + 0.5)/(df + 0.5)).
//   - SearchEngine.search (search.rs): rank documents by descending score and
//     take the top `limit`.
//
// The embedding space in codex is `usize` keyed by fxhash of the token; the hash
// is only ever used as a map key (distinct tokens -> distinct keys), so this Go
// port keys the embedding space by the token string directly. That is equivalent
// for scoring and ranking purposes.

import (
	"math"
	"sort"
)

// bm25K1 / bm25B are the bm25 crate defaults (EmbedderBuilder k1=1.2, b=0.75).
const (
	bm25K1 = 1.2
	bm25B  = 0.75
	// bm25FallbackAvgdl mirrors Embedder::FALLBACK_AVGDL, used when the corpus is
	// empty or avgdl <= 0.
	bm25FallbackAvgdl = 256.0
)

// bm25TokenEmbedding is one token occurrence in a document embedding, mirroring
// the Rust `TokenEmbedding { index, value }`. Index is the token string (the Go
// stand-in for the fxhash embedding key); value is the saturated BM25 weight.
type bm25TokenEmbedding struct {
	index string
	value float64
}

// bm25Document pairs a document id with its searchable contents. Mirrors the
// Rust `Document<K>`.
type bm25Document struct {
	id       int
	contents string
}

// bm25SearchResult is one ranked search hit. Mirrors the Rust `SearchResult<K>`.
type bm25SearchResult struct {
	id    int
	score float64
}

// bm25SearchEngine ranks documents with BM25 over the embedded corpus. Mirrors
// the Rust `SearchEngine<usize>`.
type bm25SearchEngine struct {
	avgdl float64
	// embeddings maps a document id to its token embeddings (one entry per token
	// occurrence, like the Rust Embedding vector).
	embeddings map[int][]bm25TokenEmbedding
	// docFrequency maps a token to the number of documents containing it (df).
	docFrequency map[string]int
	// docCount is the number of documents (N) in the corpus.
	docCount int
}

// newBM25SearchEngine fits an engine to the given documents, mirroring
// `SearchEngineBuilder::<usize>::with_documents(Language::English, ...).build()`.
// The avgdl is the mean token count across documents (FALLBACK_AVGDL for an
// empty corpus), and each document is upserted into the scorer.
func newBM25SearchEngine(documents []bm25Document) *bm25SearchEngine {
	avgdl := bm25FitAvgdl(documents)
	eng := &bm25SearchEngine{
		avgdl:        avgdl,
		embeddings:   make(map[int][]bm25TokenEmbedding, len(documents)),
		docFrequency: make(map[string]int),
		docCount:     0,
	}
	for _, doc := range documents {
		eng.upsert(doc)
	}
	return eng
}

// bm25FitAvgdl computes avgdl as the mean tokenized document length, mirroring
// `EmbedderBuilder::with_tokenizer_and_fit_to_corpus`. The Rust cast chain
// `(total_len as f64 / corpus.len() as f64) as f32` truncates to f32 precision.
func bm25FitAvgdl(documents []bm25Document) float64 {
	if len(documents) == 0 {
		return bm25FallbackAvgdl
	}
	var totalLen uint64
	for _, doc := range documents {
		totalLen += uint64(len(bm25Tokenize(doc.contents)))
	}
	avg := float64(totalLen) / float64(len(documents))
	return float64(float32(avg))
}

// upsert embeds a document and records it. Mirrors `SearchEngine::upsert` plus
// `Scorer::upsert`: the embedding is one TokenEmbedding per token occurrence,
// and df is incremented once per distinct token in the document.
func (e *bm25SearchEngine) upsert(doc bm25Document) {
	if _, exists := e.embeddings[doc.id]; exists {
		e.remove(doc.id)
	}
	embedding := e.embed(doc.contents)
	e.embeddings[doc.id] = embedding
	for token := range distinctTokens(embedding) {
		e.docFrequency[token]++
	}
	e.docCount++
}

// remove drops a document from the engine. Mirrors `SearchEngine::remove` plus
// `Scorer::remove`.
func (e *bm25SearchEngine) remove(id int) {
	embedding, exists := e.embeddings[id]
	if !exists {
		return
	}
	for token := range distinctTokens(embedding) {
		if e.docFrequency[token] > 0 {
			e.docFrequency[token]--
			if e.docFrequency[token] == 0 {
				delete(e.docFrequency, token)
			}
		}
	}
	delete(e.embeddings, id)
	e.docCount--
}

// distinctTokens returns the set of token indices present in an embedding,
// mirroring the per-document token set the Rust inverted index records.
func distinctTokens(embedding []bm25TokenEmbedding) map[string]struct{} {
	set := make(map[string]struct{}, len(embedding))
	for _, te := range embedding {
		set[te.index] = struct{}{}
	}
	return set
}

// embed converts text into a sparse embedding, mirroring `Embedder::embed`: one
// entry per token occurrence carrying the BM25 saturation value.
func (e *bm25SearchEngine) embed(text string) []bm25TokenEmbedding {
	tokens := bm25Tokenize(text)
	avgdl := e.avgdl
	if avgdl <= 0 {
		avgdl = bm25FallbackAvgdl
	}
	counts := make(map[string]int, len(tokens))
	for _, tok := range tokens {
		counts[tok]++
	}
	docLen := float64(len(tokens))
	embedding := make([]bm25TokenEmbedding, 0, len(tokens))
	for _, tok := range tokens {
		freq := float64(counts[tok])
		numerator := freq * (bm25K1 + 1.0)
		denominator := freq + bm25K1*(1.0-bm25B+bm25B*(docLen/avgdl))
		// Mirror the Rust f32 arithmetic precision so scores compare identically.
		value := float64(float32(numerator) / float32(denominator))
		embedding = append(embedding, bm25TokenEmbedding{index: tok, value: value})
	}
	return embedding
}

// idf returns the BM25 inverse document frequency for a token, mirroring
// `Scorer::idf`: ln(1 + (N - df + 0.5)/(df + 0.5)).
func (e *bm25SearchEngine) idf(token string) float64 {
	df := float64(e.docFrequency[token])
	numerator := float64(e.docCount) - df + 0.5
	denominator := df + 0.5
	return float64(float32(math.Log(float64(float32(1.0 + float32(numerator)/float32(denominator))))))
}

// scoreDoc scores a document against a query embedding, mirroring
// `Scorer::score_`: sum over the query's token indices of idf * document value
// (the first matching occurrence's value, since Rust uses `find`).
func (e *bm25SearchEngine) scoreDoc(docEmbedding, queryEmbedding []bm25TokenEmbedding) float64 {
	var score float32
	for _, qte := range queryEmbedding {
		tokenIDF := float32(e.idf(qte.index))
		var docValue float32
		for _, dte := range docEmbedding {
			if dte.index == qte.index {
				docValue = float32(dte.value)
				break
			}
		}
		score += tokenIDF * docValue
	}
	return float64(score)
}

// search returns up to `limit` documents ranked by descending BM25 score,
// mirroring `SearchEngine::search` -> `Scorer::matches`. Only documents that
// share at least one query token (and therefore could score > 0) are scored,
// then the candidates are stable-sorted by descending score.
//
// Tie-break: the Rust `Scorer::matches` collects candidates into a HashSet and
// stable-sorts them, so for tied scores the order is the HashSet iteration
// order. In the codex 0.136.0 binary that order is deterministic across process
// launches and, for the multi-agent corpus, equals descending document id (the
// reverse of registration order). This port reproduces it by scoring candidates
// in descending id order before the stable sort, which leaves tied documents in
// descending-id order. See DEVIATIONS.
func (e *bm25SearchEngine) search(query string, limit int) []bm25SearchResult {
	queryEmbedding := e.embed(query)
	queryTokens := distinctTokens(queryEmbedding)

	ids := make([]int, 0, len(e.embeddings))
	for id := range e.embeddings {
		ids = append(ids, id)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ids)))

	results := make([]bm25SearchResult, 0, len(ids))
	for _, id := range ids {
		embedding := e.embeddings[id]
		if !sharesToken(embedding, queryTokens) {
			continue
		}
		score := e.scoreDoc(embedding, queryEmbedding)
		results = append(results, bm25SearchResult{id: id, score: score})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if limit >= 0 && limit < len(results) {
		results = results[:limit]
	}
	return results
}

// sharesToken reports whether the document embedding contains any of the query
// tokens, mirroring the Rust inverted-index candidate filter.
func sharesToken(embedding []bm25TokenEmbedding, queryTokens map[string]struct{}) bool {
	for _, te := range embedding {
		if _, ok := queryTokens[te.index]; ok {
			return true
		}
	}
	return false
}
