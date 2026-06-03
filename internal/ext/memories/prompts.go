package memories

import (
	_ "embed"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sqlrush/codexgo/internal/utils/abspath"
	"github.com/sqlrush/codexgo/internal/utils/template"
	"github.com/sqlrush/codexgo/internal/utils/truncation"
)

//go:embed templates/memories/read_path.md
var readPathTemplateSource string

var (
	readPathTemplateOnce sync.Once
	readPathTemplate     *template.Template
	readPathTemplateErr  error
)

// memoryToolDeveloperInstructionsTemplate parses the embedded read_path.md
// template once, mirroring the LazyLock-initialized
// MEMORY_TOOL_DEVELOPER_INSTRUCTIONS_TEMPLATE.
func memoryToolDeveloperInstructionsTemplate() (*template.Template, error) {
	readPathTemplateOnce.Do(func() {
		readPathTemplate, readPathTemplateErr = template.Parse(readPathTemplateSource)
	})
	return readPathTemplate, readPathTemplateErr
}

// BuildMemoryToolDeveloperInstructions builds the memory read-path prompt added
// to developer instructions, mirroring build_memory_tool_developer_instructions.
//
// It reads codexHome/memories/memory_summary.md, trims it, truncates it to
// MemoryToolDeveloperInstructionsSummaryTokenLimit tokens, and renders the
// template. It returns ("", false, nil) when the summary file is missing or
// effectively empty.
func BuildMemoryToolDeveloperInstructions(codexHome abspath.AbsolutePathBuf) (string, bool, error) {
	basePath := codexHome.Join("memories").Path()
	memorySummaryPath := filepath.Join(basePath, "memory_summary.md")

	data, err := os.ReadFile(memorySummaryPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, errIO(err)
	}

	memorySummary := strings.TrimSpace(string(data))
	memorySummary = truncation.TruncateText(
		memorySummary,
		truncation.TokensPolicy(MemoryToolDeveloperInstructionsSummaryTokenLimit),
	)
	if memorySummary == "" {
		return "", false, nil
	}

	tmpl, err := memoryToolDeveloperInstructionsTemplate()
	if err != nil {
		return "", false, err
	}
	rendered, err := tmpl.RenderPairs([]template.Pair{
		{Name: "base_path", Value: basePath},
		{Name: "memory_summary", Value: memorySummary},
	})
	if err != nil {
		// Mirror the Rust `.ok()`, which discards render errors and yields None.
		return "", false, nil
	}
	return rendered, true, nil
}
