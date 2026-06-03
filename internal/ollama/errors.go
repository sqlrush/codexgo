package ollama

import "errors"

// ollamaConnectionError is the message surfaced when no local Ollama server can
// be reached. It mirrors the Rust OLLAMA_CONNECTION_ERROR constant verbatim so
// users see identical guidance.
const ollamaConnectionError = "No running Ollama server detected. Start it with: `ollama serve` (after installing). Install instructions: https://github.com/ollama/ollama?tab=readme-ov-file#ollama"

// errNegativeNumber is returned internally when a JSON number that should be a
// non-negative byte count is negative; callers treat it as "value absent",
// matching serde_json's as_u64 behavior.
var errNegativeNumber = errors.New("ollama: negative number")
