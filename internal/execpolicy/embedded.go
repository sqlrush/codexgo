package execpolicy

import _ "embed"

// ExamplePolicyIdentifier is the identifier used when parsing the bundled
// example policy. It matches the filename Codex ships the policy under.
const ExamplePolicyIdentifier = "example.codexpolicy"

// ExamplePolicy is the baseline example policy Codex bundles with the execpolicy
// crate (`execpolicy/examples/example.codexpolicy`), embedded verbatim. It
// illustrates the policy language and is not comprehensive nor recommended for
// production use, matching the comment in the source file.
//
//go:embed example.codexpolicy
var ExamplePolicy string

// LoadExamplePolicy parses the bundled [ExamplePolicy] and returns the resulting
// [Policy]. Because the example declares match/not_match self-tests, this also
// exercises the load-time validation, so a non-nil error indicates the embedded
// asset drifted from the engine's matching semantics.
func LoadExamplePolicy() (*Policy, error) {
	parser := NewPolicyParser()
	if err := parser.Parse(ExamplePolicyIdentifier, ExamplePolicy); err != nil {
		return nil, err
	}
	return parser.Build(), nil
}
