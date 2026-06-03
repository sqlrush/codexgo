package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// SelectionAction is a callback invoked when a [SelectionItem] is accepted. It
// receives the sender so it can forward an engine op or open a child overlay.
//
// Port of list_selection_view.rs SelectionAction (Box<dyn Fn(&AppEventSender)>).
type SelectionAction func(*AppEventSender)

// SelectionToggle holds the on/off state and action for a toggleable item.
//
// Port of list_selection_view.rs SelectionToggle.
type SelectionToggle struct {
	IsOn   bool
	Action func(on bool, sender *AppEventSender)
}

// SelectionItem is one row in a [ListSelectionOverlay].
//
// Port of list_selection_view.rs SelectionItem, reduced to the fields the
// behavioral port consumes.
type SelectionItem struct {
	Name            string
	Description     string
	DisplayShortcut string
	IsCurrent       bool
	IsDisabled      bool
	DisabledReason  string
	SearchValue     string
	DismissOnSelect bool
	Toggle          *SelectionToggle
	Actions         []SelectionAction
}

func (it SelectionItem) enabled() bool {
	return !it.IsDisabled && it.DisabledReason == ""
}

// SelectionViewParams configures a [ListSelectionOverlay].
//
// Port of list_selection_view.rs SelectionViewParams, reduced to the
// behaviorally relevant fields.
type SelectionViewParams struct {
	ViewID            string
	Title             string
	Subtitle          string
	FooterHint        string
	Items             []SelectionItem
	IsSearchable      bool
	SearchPlaceholder string
	HeaderLines       []string
	InitialSelected   int // -1 for none
	// OnSelectionChanged fires when the highlighted item changes; receives the
	// actual item index. Used for live preview (e.g. theme picker).
	OnSelectionChanged func(actualIdx int, sender *AppEventSender)
	// OnCancel fires when the view is dismissed without accepting.
	OnCancel func(sender *AppEventSender)
}

// ListSelectionOverlay is the generic list-based selection popup used by the
// model/theme/permissions/skills/plugins pickers and as the rendering engine for
// the approval modal.
//
// Port of list_selection_view.rs ListSelectionView.
type ListSelectionOverlay struct {
	viewID            string
	title             string
	subtitle          string
	footerHint        string
	headerLines       []string
	items             []SelectionItem
	state             ScrollState
	completion        ViewCompletion
	sender            *AppEventSender
	isSearchable      bool
	searchQuery       string
	searchPlaceholder string
	filtered          []int // actual indices currently visible
	lastSelected      int   // -1 when none
	initialSelected   int
	onSelectionChange func(int, *AppEventSender)
	onCancel          func(*AppEventSender)
}

// NewListSelectionOverlay builds a list selection overlay from params. A
// negative params.InitialSelected means "no initial selection"; zero and
// positive values are treated as a valid item index.
func NewListSelectionOverlay(params SelectionViewParams, sender *AppEventSender) *ListSelectionOverlay {
	v := &ListSelectionOverlay{
		viewID:            params.ViewID,
		title:             params.Title,
		subtitle:          params.Subtitle,
		footerHint:        params.FooterHint,
		headerLines:       params.HeaderLines,
		items:             params.Items,
		state:             NewScrollState(),
		completion:        CompletionNone,
		sender:            sender,
		isSearchable:      params.IsSearchable,
		searchPlaceholder: params.SearchPlaceholder,
		lastSelected:      -1,
		initialSelected:   params.InitialSelected,
		onSelectionChange: params.OnSelectionChanged,
		onCancel:          params.OnCancel,
	}
	if !params.IsSearchable {
		v.searchPlaceholder = ""
	}
	v.applyFilter()
	return v
}

// ViewID returns the view's stable id for external refreshes.
func (v *ListSelectionOverlay) ViewID() string { return v.viewID }

// SelectedIndex returns the actual selected item index, or -1.
func (v *ListSelectionOverlay) SelectedIndex() int { return v.selectedActualIdx() }

// TakeLastSelectedIndex returns and clears the last accepted item index.
//
// Port of ListSelectionView::take_last_selected_index. Used by the approval
// overlay to learn which option was chosen.
func (v *ListSelectionOverlay) TakeLastSelectedIndex() (int, bool) {
	if v.lastSelected < 0 {
		return -1, false
	}
	idx := v.lastSelected
	v.lastSelected = -1
	return idx, true
}

func (v *ListSelectionOverlay) visibleLen() int { return len(v.filtered) }

func (v *ListSelectionOverlay) maxVisibleRows() int {
	n := v.visibleLen()
	if n < 1 {
		n = 1
	}
	if n > MaxPopupRows {
		return MaxPopupRows
	}
	return n
}

func (v *ListSelectionOverlay) selectedActualIdx() int {
	if !v.state.HasSelection() {
		return -1
	}
	if v.state.Selected >= len(v.filtered) {
		return -1
	}
	return v.filtered[v.state.Selected]
}

func (v *ListSelectionOverlay) applyFilter() {
	prev := v.selectedActualIdx()
	if prev >= 0 && !v.items[prev].enabled() {
		prev = -1
	}
	if prev < 0 && !v.isSearchable {
		for i, it := range v.items {
			if it.IsCurrent && it.enabled() {
				prev = i
				break
			}
		}
	}
	if prev < 0 && v.initialSelected >= 0 && v.initialSelected < len(v.items) {
		if v.items[v.initialSelected].enabled() {
			prev = v.initialSelected
		}
		v.initialSelected = -1
	}

	v.filtered = v.filtered[:0]
	if v.isSearchable && v.searchQuery != "" {
		q := strings.ToLower(v.searchQuery)
		for i, it := range v.items {
			if it.SearchValue != "" && strings.Contains(strings.ToLower(it.SearchValue), q) {
				v.filtered = append(v.filtered, i)
			}
		}
	} else {
		for i := range v.items {
			v.filtered = append(v.filtered, i)
		}
	}

	// Re-resolve the selected visible index to the previously selected actual.
	sel := -1
	if prev >= 0 {
		for vi, actual := range v.filtered {
			if actual == prev {
				sel = vi
				break
			}
		}
	}
	if sel >= 0 && v.items[v.filtered[sel]].enabled() {
		v.state.Selected = sel
	} else {
		v.state.Selected = v.firstEnabledVisible()
	}
	v.state.EnsureVisible(v.visibleLen(), v.maxVisibleRows())
}

func (v *ListSelectionOverlay) firstEnabledVisible() int {
	for vi, actual := range v.filtered {
		if v.items[actual].enabled() {
			return vi
		}
	}
	return -1
}

func (v *ListSelectionOverlay) fireSelectionChanged() {
	if v.onSelectionChange == nil {
		return
	}
	if actual := v.selectedActualIdx(); actual >= 0 {
		v.onSelectionChange(actual, v.sender)
	}
}

func (v *ListSelectionOverlay) moveUp() {
	before := v.selectedActualIdx()
	v.state.MoveUpWrap(v.visibleLen())
	v.skipDisabled(-1)
	v.state.EnsureVisible(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

func (v *ListSelectionOverlay) moveDown() {
	before := v.selectedActualIdx()
	v.state.MoveDownWrap(v.visibleLen())
	v.skipDisabled(1)
	v.state.EnsureVisible(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

// skipDisabled walks in dir (-1 up, +1 down) past disabled rows, wrapping.
func (v *ListSelectionOverlay) skipDisabled(dir int) {
	n := v.visibleLen()
	for i := 0; i < n; i++ {
		if !v.visibleDisabled(v.state.Selected) {
			return
		}
		if dir < 0 {
			v.state.MoveUpWrap(n)
		} else {
			v.state.MoveDownWrap(n)
		}
	}
}

func (v *ListSelectionOverlay) visibleDisabled(idx int) bool {
	if idx < 0 || idx >= len(v.filtered) {
		return false
	}
	return !v.items[v.filtered[idx]].enabled()
}

func (v *ListSelectionOverlay) accept() {
	actual := v.selectedActualIdx()
	if actual >= 0 && v.items[actual].enabled() {
		v.lastSelected = actual
		item := v.items[actual]
		for _, act := range item.Actions {
			act(v.sender)
		}
		if item.DismissOnSelect {
			v.completion = CompletionAccepted
		}
		return
	}
	if actual < 0 {
		if v.onCancel != nil {
			v.onCancel(v.sender)
		}
		v.completion = CompletionCancelled
	}
}

func (v *ListSelectionOverlay) toggleSelected() {
	actual := v.selectedActualIdx()
	if actual < 0 || !v.items[actual].enabled() {
		return
	}
	tog := v.items[actual].Toggle
	if tog == nil {
		return
	}
	tog.IsOn = !tog.IsOn
	if tog.Action != nil {
		tog.Action(tog.IsOn, v.sender)
	}
}

// HandleKey implements OverlayView.
func (v *ListSelectionOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()
	plainText := isPlainTextKey(msg)
	allowNav := !v.isSearchable || !plainText

	switch {
	case allowNav && (key == "up" || key == "ctrl+p"):
		v.moveUp()
	case allowNav && (key == "down" || key == "ctrl+n"):
		v.moveDown()
	case allowNav && key == "pgup":
		v.pageUp()
	case allowNav && key == "pgdown":
		v.pageDown()
	case allowNav && (key == "home" || key == "ctrl+a"):
		v.jumpTop()
	case allowNav && (key == "end" || key == "ctrl+e"):
		v.jumpBottom()
	case allowNav && key == "k":
		v.moveUp()
	case allowNav && key == "j":
		v.moveDown()
	case v.isSearchable && key == "backspace":
		if len(v.searchQuery) > 0 {
			runes := []rune(v.searchQuery)
			v.searchQuery = string(runes[:len(runes)-1])
			v.applyFilter()
		}
	case key == " " && v.selectedItemHasToggle() && (!v.isSearchable || v.searchQuery == ""):
		v.toggleSelected()
	case key == "esc":
		v.OnCtrlC()
	case key == "enter":
		v.accept()
	case v.isSearchable && len(msg.Runes) == 1 && plainText:
		v.searchQuery += string(msg.Runes)
		v.applyFilter()
	case !v.isSearchable && len(msg.Runes) == 1 && plainText:
		v.handleShortcutOrNumber(msg)
	}
	return nil
}

func (v *ListSelectionOverlay) handleShortcutOrNumber(msg tea.KeyMsg) {
	r := msg.Runes[0]
	// Display shortcut match.
	for i, it := range v.items {
		if it.DisplayShortcut != "" && it.DisplayShortcut == string(r) && it.enabled() {
			if vi := v.visibleIndexOf(i); vi >= 0 {
				v.state.Selected = vi
				v.accept()
				return
			}
		}
	}
	// Number key jumps to the Nth enabled item.
	if r >= '1' && r <= '9' {
		number := int(r - '0')
		if actual := v.actualIdxForEnabledNumber(number); actual >= 0 {
			if vi := v.visibleIndexOf(actual); vi >= 0 {
				v.state.Selected = vi
				v.accept()
			}
		}
	}
}

func (v *ListSelectionOverlay) visibleIndexOf(actual int) int {
	for vi, a := range v.filtered {
		if a == actual {
			return vi
		}
	}
	return -1
}

func (v *ListSelectionOverlay) actualIdxForEnabledNumber(number int) int {
	if number == 0 {
		return -1
	}
	count := 0
	for i, it := range v.items {
		if it.enabled() {
			count++
			if count == number {
				return i
			}
		}
	}
	return -1
}

func (v *ListSelectionOverlay) selectedItemHasToggle() bool {
	actual := v.selectedActualIdx()
	if actual < 0 {
		return false
	}
	return v.items[actual].Toggle != nil && v.items[actual].enabled()
}

func (v *ListSelectionOverlay) pageUp() {
	before := v.selectedActualIdx()
	v.state.PageUpClamped(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

func (v *ListSelectionOverlay) pageDown() {
	before := v.selectedActualIdx()
	v.state.PageDownClamped(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

func (v *ListSelectionOverlay) jumpTop() {
	before := v.selectedActualIdx()
	v.state.JumpTop(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

func (v *ListSelectionOverlay) jumpBottom() {
	before := v.selectedActualIdx()
	v.state.JumpBottom(v.visibleLen(), v.maxVisibleRows())
	if v.selectedActualIdx() != before {
		v.fireSelectionChanged()
	}
}

// OnCtrlC implements overlayCtrlC.
func (v *ListSelectionOverlay) OnCtrlC() CancellationEvent {
	if v.onCancel != nil {
		v.onCancel(v.sender)
	}
	v.completion = CompletionCancelled
	return CancellationHandled
}

// PrefersEscToHandleKey implements overlayPrefersEscToKey.
func (v *ListSelectionOverlay) PrefersEscToHandleKey() bool { return true }

// IsComplete implements OverlayView.
func (v *ListSelectionOverlay) IsComplete() bool { return v.completion != CompletionNone }

// Completion implements overlayCompletion.
func (v *ListSelectionOverlay) Completion() ViewCompletion { return v.completion }

// DesiredHeight implements OverlayView.
func (v *ListSelectionOverlay) DesiredHeight(width int) int {
	h := len(v.headerLines)
	if v.title != "" {
		h++
	}
	if v.subtitle != "" {
		h++
	}
	rows := v.visibleLen()
	if rows > MaxPopupRows {
		rows = MaxPopupRows
	}
	if rows == 0 {
		rows = 1
	}
	h += rows
	if v.footerHint != "" {
		h++
	}
	if v.isSearchable {
		h++
	}
	return h
}

// View implements OverlayView.
func (v *ListSelectionOverlay) View(theme Theme, area Rect) string {
	var b strings.Builder
	for _, line := range v.headerLines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if v.title != "" {
		b.WriteString(theme.UserMessage.Render(v.title))
		b.WriteByte('\n')
	}
	if v.subtitle != "" {
		b.WriteString(theme.Dimmed().Render(v.subtitle))
		b.WriteByte('\n')
	}
	if v.isSearchable {
		query := v.searchQuery
		if query == "" && v.searchPlaceholder != "" {
			query = theme.Dimmed().Render(v.searchPlaceholder)
		}
		b.WriteString(theme.Dimmed().Render("Search: ") + query)
		b.WriteByte('\n')
	}

	rows := make([]DisplayRow, 0, len(v.filtered))
	for _, actual := range v.filtered {
		it := v.items[actual]
		name := it.Name
		if it.Toggle != nil {
			if it.Toggle.IsOn {
				name = "[x] " + name
			} else {
				name = "[ ] " + name
			}
		}
		if it.IsCurrent {
			name += " (current)"
		}
		rows = append(rows, DisplayRow{
			Name:            name,
			Description:     it.Description,
			DisplayShortcut: it.DisplayShortcut,
			IsDisabled:      !it.enabled(),
			DisabledReason:  it.DisabledReason,
		})
	}
	b.WriteString(renderRows(theme, rows, v.state, MaxPopupRows, "no matches"))
	if v.footerHint != "" {
		b.WriteByte('\n')
		b.WriteString(theme.Dimmed().Render(v.footerHint))
	}
	return b.String()
}

// isPlainTextKey reports whether the key event is a printable character with no
// control/alt modifiers (so searchable lists capture it as query input).
func isPlainTextKey(msg tea.KeyMsg) bool {
	if msg.Type != tea.KeyRunes {
		return false
	}
	if msg.Alt {
		return false
	}
	return len(msg.Runes) == 1
}

var _ OverlayView = (*ListSelectionOverlay)(nil)
