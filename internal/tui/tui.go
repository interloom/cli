// Package tui implements an interactive terminal UI for browsing spaces and
// exploring case trees. The browse screen lists spaces and their cases; opening
// a case drills into a case-detail screen that shows that single case and its
// sub-cases, files and notes. Selecting a sub-case drills another level deeper,
// so whole case trees can be navigated; Esc walks back up. Note bodies and case
// descriptions render as markdown. It is a thin, read-only viewer over the same
// REST client the rest of the CLI uses.
//
// Lists are cursor-paginated: each pane loads one page at a time and prefetches
// the next as you scroll near the end. Navigating quickly cancels superseded
// in-flight requests so only the latest selection's data is fetched. Cases and
// sub-cases can be filtered by status (client-side, since the API has no
// status filter).
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/interloom/cli/internal/api"
	"github.com/interloom/cli/internal/client"
	"github.com/interloom/cli/internal/config"
	"golang.org/x/term"
)

// Run starts the TUI and blocks until the user quits. cfgName is the active
// config shown in the header and preselected in the config switcher.
func Run(ctx context.Context, c *client.Client, cfgName string) error {
	p := tea.NewProgram(
		newModel(ctx, c, cfgName),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
		tea.WithContext(ctx),
	)
	_, err := p.Run()
	return err
}

// focus names a real UI target (a list pane or the detail pane), not a column
// index — the set of visible panes changes between screens.
type focus int

const (
	focusSpaces focus = iota
	focusCases
	focusSubcases
	focusFiles
	focusNotes
	focusDetail

	// focusCurrentCase is not a navigable pane; it is the detail-source value
	// that means "show the case currently being viewed" in the case screen.
	focusCurrentCase focus = -1
)

// phase is the top-level UI state: an animated splash, the spaces/cases
// browser, or the single-case detail screen.
type phase int

const (
	phaseIntro phase = iota
	phaseBrowse
	phaseCase
)

// Intro animation timing: a high frame rate so the time-based splash animation
// (eased fade-ins, a sheen sweeping the logo, a per-letter reveal and looping
// shimmer) stays fluid. The animation runs indefinitely until Enter is pressed.
const introInterval = 33 * time.Millisecond // ~30fps

// Common key names reused across handlers.
const (
	keyCtrlC = "ctrl+c"
	keyEsc   = "esc"
	keyEnter = "enter"
)

// loomSpinner animates the brand mark: the inner threads weave together and
// open back up. Every frame is 4 cells wide so it never jitters.
var loomSpinner = spinner.Spinner{
	Frames: []string{">)(<", `>/\<`, ">||<", `>/\<`},
	FPS:    time.Second / 6,
}

// statusCycle is the order the status filter steps through; "" means "all".
var statusCycle = []api.CaseStatus{"", api.Open, api.Started, api.Blocked, api.Completed, api.Cancelled}

type model struct {
	ctx    context.Context
	client *client.Client

	width, height int
	phase         phase
	introFrame    int
	focus         focus
	detailSource  focus // what the detail pane renders (a pane, or focusCurrentCase)
	rawMode       bool  // raw source instead of rendered markdown (notes, descriptions, text files)

	statusFilter api.CaseStatus // "" == all; applied to cases and sub-cases

	// Debug request log (toggled with `d`): every API request is tracked with
	// its label, status and duration, even while the panel is hidden.
	debug  bool
	reqSeq int        // monotonic request id
	reqLog []reqEntry // bounded, oldest-first

	// Config switcher (a "config use" picker, opened with `c`).
	cfgName       string   // active config, shown in the header
	cfgPickerOpen bool     // the switcher modal is open
	cfgNames      []string // available configs, loaded when the picker opens
	cfgCur        int      // selected row in the picker
	cfgErr        error    // last list/switch error, shown in the picker

	// Browse screen: spaces and the cases of the selected space.
	sp paged[api.SpaceListItem]
	cs paged[api.CaseListItem]

	// Case screen: the children of the case at the top of caseStack.
	caseStack []api.CaseListItem // root → current; last element is the current case
	scs       paged[api.CaseListItem]
	fl        paged[api.File]
	nt        paged[api.NoteListItem]

	// Full-object caches for the detail panel, keyed by id, plus the in-flight
	// set. detailGen invalidates stale detail responses after a reload/switch.
	caseDetail        map[string]*api.Case
	noteDetail        map[string]*api.Note
	caseDetailLoading map[string]bool
	noteDetailLoading map[string]bool
	detailGen         int
	users             map[string]api.User // assignee id → display data

	// Inline image previews (Kitty graphics protocol). Prepared images are
	// cached by file id; viewFile holds the file whose download we're awaiting
	// so we can launch the viewer the moment its bytes arrive.
	imgCache   map[string]imagePrepared
	imgLoading map[string]bool
	imgErr     map[string]error
	viewFile   *api.File

	// Inline text/eml previews, lazily loaded and cached by file id.
	fileText        map[string]textContent
	fileTextLoading map[string]bool
	fileTextErr     map[string]error

	spin spinner.Model
	vp   viewport.Model

	md      *glamour.TermRenderer
	mdWidth int

	lastDetailKey string // memoizes the rendered detail panel
	detailMdOn    bool   // whether the detail body is currently markdown-rendered

	err error
}

func newModel(ctx context.Context, c *client.Client, cfgName string) model {
	sp := spinner.New()
	sp.Spinner = loomSpinner
	sp.Style = brandSt
	m := model{
		ctx:               ctx,
		client:            c,
		cfgName:           cfgName,
		focus:             focusSpaces,
		caseDetail:        map[string]*api.Case{},
		noteDetail:        map[string]*api.Note{},
		caseDetailLoading: map[string]bool{},
		noteDetailLoading: map[string]bool{},
		users:             map[string]api.User{},
		imgCache:          map[string]imagePrepared{},
		imgLoading:        map[string]bool{},
		imgErr:            map[string]error{},
		fileText:          map[string]textContent{},
		fileTextLoading:   map[string]bool{},
		fileTextErr:       map[string]error{},
		spin:              sp,
	}
	// Prime the initial requests so their generations match the responses and
	// both requests show up in the debug panel from the first frame.
	m.sp.gen = 1
	m.sp.loading = true
	m.reqSeq = 2
	now := time.Now()
	m.reqLog = []reqEntry{
		{id: 1, label: "spaces", started: now, status: reqPending},
		{id: 2, label: "users", started: now, status: reqPending},
	}

	// Seed the terminal size now so the very first frame is the animated
	// splash, not a bare "starting…" line. Bubbletea still sends a
	// WindowSizeMsg once it queries the terminal (and corrects us), but
	// seeding here also keeps the UI usable in the rare case that query never
	// arrives — e.g. when stdout is not a clean TTY.
	m.width, m.height = initialTermSize()
	m.applyLayout()
	return m
}

// initialTermSize reports the current terminal size, falling back to a sane
// default when stdout is not a measurable TTY.
func initialTermSize() (int, int) {
	if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 && h > 0 {
		return w, h
	}
	return 80, 24
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,
		introTick(),
		loadPage(m.ctx, kindSpaces, m.sp.gen, 1, "", "", false, m.spacesFetcher()),
		loadUsersPage(m.ctx, m.client, m.detailGen, 2, "", 1, nil),
	)
}

type usersLoadedMsg struct {
	gen     int
	reqID   int
	page    int
	count   int
	users   []api.User
	next    string
	hasMore bool
	err     error
}

func loadUsersPage(ctx context.Context, c *client.Client, gen, reqID int, cursor string, page int, users []api.User) tea.Cmd {
	return func() tea.Msg {
		items, next, hasMore, err := fetchUsersPage(ctx, c, cursor)
		users = append(users, items...)
		return usersLoadedMsg{
			gen: gen, reqID: reqID, page: page, count: len(items), users: users,
			next: next, hasMore: hasMore, err: err,
		}
	}
}

func (m *model) issueUsers(cursor string, page int, users []api.User) tea.Cmd {
	label := "users"
	if page > 1 {
		label += fmt.Sprintf(" · page %d", page)
	}
	reqID := m.logStart(label)
	return loadUsersPage(m.ctx, m.client, m.detailGen, reqID, cursor, page, users)
}

type introTickMsg struct{}

func introTick() tea.Cmd {
	return tea.Tick(introInterval, func(time.Time) tea.Msg { return introTickMsg{} })
}

// ---- active panes ----

// listPanes returns the navigable list panes for the current screen, in order.
func (m model) listPanes() []focus {
	if m.phase == phaseCase {
		return []focus{focusSubcases, focusFiles, focusNotes}
	}
	return []focus{focusSpaces, focusCases}
}

// focusOrder is the list panes plus the detail pane, the Tab cycle order.
func (m model) focusOrder() []focus {
	return append(m.listPanes(), focusDetail)
}

// cycleFocus advances the focused pane by delta within the current screen.
func (m *model) cycleFocus(delta int) {
	order := m.focusOrder()
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(order)) % len(order)
	m.setFocus(order[idx])
}

// ---- page fetchers. Cases and sub-cases are filtered server-side by the
// active status filter (passed through as the `status` query param). ----

func (m model) spacesFetcher() pageFetcher[api.SpaceListItem] {
	c := m.client
	return func(ctx context.Context, cursor string) ([]api.SpaceListItem, string, bool, error) {
		return fetchSpacesPage(ctx, c, cursor)
	}
}

func (m model) casesFetcher(spaceID string) pageFetcher[api.CaseListItem] {
	c, status := m.client, string(m.statusFilter)
	return func(ctx context.Context, cursor string) ([]api.CaseListItem, string, bool, error) {
		return fetchCasesPage(ctx, c, spaceID, status, cursor)
	}
}

func (m model) subcasesFetcher(parentCaseID string) pageFetcher[api.CaseListItem] {
	c, status := m.client, string(m.statusFilter)
	return func(ctx context.Context, cursor string) ([]api.CaseListItem, string, bool, error) {
		return fetchSubCasesPage(ctx, c, parentCaseID, status, cursor)
	}
}

func (m model) filesFetcher(caseID string) pageFetcher[api.File] {
	c := m.client
	return func(ctx context.Context, cursor string) ([]api.File, string, bool, error) {
		return fetchFilesPage(ctx, c, caseID, cursor)
	}
}

func (m model) notesFetcher(caseID string) pageFetcher[api.NoteListItem] {
	c := m.client
	return func(ctx context.Context, cursor string) ([]api.NoteListItem, string, bool, error) {
		return fetchNotesPage(ctx, c, caseID, cursor)
	}
}

func nextStatus(s api.CaseStatus) api.CaseStatus {
	for i, v := range statusCycle {
		if v == s {
			return statusCycle[(i+1)%len(statusCycle)]
		}
	}
	return ""
}

// ---- load commands (cancel the previous request, bump generation) ----

func (m *model) issueSpaces(cursor string, appendPage bool) tea.Cmd {
	ctx := m.newCtx(&m.sp.cancel)
	m.sp.gen++
	m.sp.setLoading(appendPage)
	id := m.logStart(pageLabel("spaces", "", appendPage, m.sp.pagesFetched))
	return loadPage(ctx, kindSpaces, m.sp.gen, id, "", cursor, appendPage, m.spacesFetcher())
}

func (m *model) issueCases(cursor string, appendPage bool) tea.Cmd {
	ctx := m.newCtx(&m.cs.cancel)
	m.cs.gen++
	m.cs.setLoading(appendPage)
	id := m.logStart(pageLabel("cases", m.cs.parentID, appendPage, m.cs.pagesFetched))
	return loadPage(ctx, kindCases, m.cs.gen, id, m.cs.parentID, cursor, appendPage, m.casesFetcher(m.cs.parentID))
}

func (m *model) issueSubcases(cursor string, appendPage bool) tea.Cmd {
	ctx := m.newCtx(&m.scs.cancel)
	m.scs.gen++
	m.scs.setLoading(appendPage)
	id := m.logStart(pageLabel("sub-cases", m.scs.parentID, appendPage, m.scs.pagesFetched))
	return loadPage(ctx, kindSubcases, m.scs.gen, id, m.scs.parentID, cursor, appendPage, m.subcasesFetcher(m.scs.parentID))
}

func (m *model) issueFiles(cursor string, appendPage bool) tea.Cmd {
	ctx := m.newCtx(&m.fl.cancel)
	m.fl.gen++
	m.fl.setLoading(appendPage)
	id := m.logStart(pageLabel("files", m.fl.parentID, appendPage, m.fl.pagesFetched))
	return loadPage(ctx, kindFiles, m.fl.gen, id, m.fl.parentID, cursor, appendPage, m.filesFetcher(m.fl.parentID))
}

func (m *model) issueNotes(cursor string, appendPage bool) tea.Cmd {
	ctx := m.newCtx(&m.nt.cancel)
	m.nt.gen++
	m.nt.setLoading(appendPage)
	id := m.logStart(pageLabel("notes", m.nt.parentID, appendPage, m.nt.pagesFetched))
	return loadPage(ctx, kindNotes, m.nt.gen, id, m.nt.parentID, cursor, appendPage, m.notesFetcher(m.nt.parentID))
}

// newCtx cancels whatever *slot points at and stores a fresh cancel there.
func (m *model) newCtx(slot *context.CancelFunc) context.Context {
	if *slot != nil {
		(*slot)()
	}
	ctx, cancel := context.WithCancel(m.ctx)
	*slot = cancel
	return ctx
}

// loadCaseDetail fetches a case's full detail once, keyed by id. Stale or
// duplicate fetches are skipped; detailGen drops responses from before a reload.
func (m *model) loadCaseDetail(id string) tea.Cmd {
	if id == "" || m.caseDetail[id] != nil || m.caseDetailLoading[id] {
		return nil
	}
	m.caseDetailLoading[id] = true
	reqID := m.logStart("case detail:" + shortID(id))
	gen, c, ctx := m.detailGen, m.client, m.ctx
	return func() tea.Msg {
		cs, err := fetchCase(ctx, c, id)
		return caseDetailMsg{gen: gen, reqID: reqID, id: id, c: cs, err: err}
	}
}

func (m *model) loadNoteDetail(id string) tea.Cmd {
	if id == "" || m.noteDetail[id] != nil || m.noteDetailLoading[id] {
		return nil
	}
	m.noteDetailLoading[id] = true
	reqID := m.logStart("note detail:" + shortID(id))
	gen, c, ctx := m.detailGen, m.client, m.ctx
	return func() tea.Msg {
		n, err := fetchNote(ctx, c, id)
		return noteDetailMsg{gen: gen, reqID: reqID, id: id, n: n, err: err}
	}
}

// viewImage previews a file as an inline image. Cached images launch the
// viewer immediately; otherwise the download is kicked off and remembered
// (viewFile) so the viewer fires as soon as the bytes arrive.
func (m model) viewImage(f api.File) (tea.Model, tea.Cmd) {
	id := f.Id.String()
	if p, ok := m.imgCache[id]; ok {
		return m, m.execImage(f, p)
	}
	ff := f
	m.viewFile = &ff
	if m.imgLoading[id] {
		return m, nil // a load is already in flight; it will launch the viewer
	}
	return m, m.loadImage(f)
}

// loadImage downloads and decodes a file's bytes off the UI goroutine, once per
// file id (cached images and in-flight loads are skipped; errors can retry).
func (m *model) loadImage(f api.File) tea.Cmd {
	id := f.Id.String()
	if _, ok := m.imgCache[id]; ok || m.imgLoading[id] {
		return nil
	}
	m.imgLoading[id] = true
	delete(m.imgErr, id)
	reqID := m.logStart("image:" + shortID(id))
	gen, ctx, url := m.detailGen, m.ctx, f.DownloadUrl
	return func() tea.Msg {
		data, err := downloadBytes(ctx, url, maxImageBytes)
		if err != nil {
			return imageLoadedMsg{gen: gen, reqID: reqID, id: id, err: err}
		}
		img, err := prepareImage(data)
		return imageLoadedMsg{gen: gen, reqID: reqID, id: id, img: img, err: err}
	}
}

// loadFileText downloads and prepares a text/eml file's content for inline
// display, once per file id. Non-text or oversized files are ignored.
func (m *model) loadFileText(f api.File) tea.Cmd {
	if !textCandidate(f.MimeType, f.Name, f.Size) {
		return nil
	}
	id := f.Id.String()
	if _, ok := m.fileText[id]; ok || m.fileTextLoading[id] {
		return nil
	}
	m.fileTextLoading[id] = true
	delete(m.fileTextErr, id)
	reqID := m.logStart("filetext:" + shortID(id))
	gen, ctx := m.detailGen, m.ctx
	url, mimeType, name := f.DownloadUrl, f.MimeType, f.Name
	return func() tea.Msg {
		data, err := downloadBytes(ctx, url, maxTextBytes)
		if err != nil {
			return fileTextMsg{gen: gen, reqID: reqID, id: id, err: err}
		}
		return fileTextMsg{gen: gen, reqID: reqID, id: id, tc: prepareText(data, mimeType, name)}
	}
}

type fileTextMsg struct {
	gen   int
	reqID int
	id    string
	tc    textContent
	err   error
}

// execImage hands the terminal to the Kitty-graphics image viewer.
func (m model) execImage(f api.File, p imagePrepared) tea.Cmd {
	v := &imageViewer{
		img:  p,
		name: f.Name,
		info: fmt.Sprintf("%s · %s · %d×%d", f.MimeType, humanSize(f.Size), p.w, p.h),
	}
	return tea.Exec(v, func(error) tea.Msg { return imageClosedMsg{} })
}

type imageLoadedMsg struct {
	gen   int
	reqID int
	id    string
	img   imagePrepared
	err   error
}

// imageClosedMsg is delivered after the viewer hands the terminal back.
type imageClosedMsg struct{}

type caseDetailMsg struct {
	gen   int
	reqID int
	id    string
	c     *api.Case
	err   error
}
type noteDetailMsg struct {
	gen   int
	reqID int
	id    string
	n     *api.Note
	err   error
}

// ---- selection helpers ----

func (m *model) curSpaceID() string {
	if s, ok := m.sp.current(); ok {
		return s.Id.String()
	}
	return ""
}

func (m *model) curCaseID() string {
	if c, ok := m.cs.current(); ok {
		return c.Id.String()
	}
	return ""
}

func (m *model) curSubcaseID() string {
	if c, ok := m.scs.current(); ok {
		return c.Id.String()
	}
	return ""
}

func (m *model) curNoteID() string {
	if n, ok := m.nt.current(); ok {
		return n.Id.String()
	}
	return ""
}

// currentCaseID is the id of the case being viewed in the case screen.
func (m *model) currentCaseID() string {
	if len(m.caseStack) == 0 {
		return ""
	}
	return m.caseStack[len(m.caseStack)-1].Id.String()
}

// onSpaceChange resets and loads the first page of cases for the current space.
func (m *model) onSpaceChange() tea.Cmd {
	id := m.curSpaceID()
	m.cs.reset(id)
	if id == "" {
		return nil
	}
	return m.issueCases("", false)
}

// onBrowseCaseChange prefetches the selected browse case's detail for preview.
func (m *model) onBrowseCaseChange() tea.Cmd {
	return m.loadCaseDetail(m.curCaseID())
}

// onCurrentCaseChange (re)loads the children of the current case and shows it.
func (m *model) onCurrentCaseChange() tea.Cmd {
	id := m.currentCaseID()
	m.scs.reset(id)
	m.fl.reset(id)
	m.nt.reset(id)
	m.detailSource = focusCurrentCase
	m.lastDetailKey = ""
	if id == "" {
		m.refreshDetail()
		return nil
	}
	cmd := tea.Batch(
		m.issueSubcases("", false),
		m.issueFiles("", false),
		m.issueNotes("", false),
		m.loadCaseDetail(id),
	)
	m.refreshDetail()
	return cmd
}

// openCase drills from the browse screen into a case's detail view.
func (m model) openCase(c api.CaseListItem) (tea.Model, tea.Cmd) {
	m.phase = phaseCase
	m.caseStack = []api.CaseListItem{c}
	m.focus = focusSubcases
	return m, m.onCurrentCaseChange()
}

// drillInto pushes a sub-case onto the stack, making it the current case.
func (m model) drillInto(c api.CaseListItem) (tea.Model, tea.Cmd) {
	m.caseStack = append(m.caseStack, c)
	m.focus = focusSubcases
	return m, m.onCurrentCaseChange()
}

// previewLoad fetches the detail for a pane's current selection, if any.
func (m *model) previewLoad(f focus) tea.Cmd {
	switch f {
	case focusCases:
		return m.loadCaseDetail(m.curCaseID())
	case focusSubcases:
		return m.loadCaseDetail(m.curSubcaseID())
	case focusNotes:
		return m.loadNoteDetail(m.curNoteID())
	case focusFiles:
		file, ok := m.fl.current()
		if !ok {
			return nil
		}
		if imageSupported() && isImageMime(file.MimeType) {
			return m.loadImage(file) // bytes cached; rendered inline in the detail pane
		}
		return m.loadFileText(file)
	default:
		return nil
	}
}

// previewFocused points the detail pane at the focused pane's selection.
func (m *model) previewFocused() tea.Cmd {
	m.detailSource = m.focus
	return m.previewLoad(m.focus)
}

// advanceIntro steps the splash animation. It loops forever while the intro is
// on screen; the user dismisses it by pressing Enter (see handleIntroKey).
func (m model) advanceIntro() (tea.Model, tea.Cmd) {
	if m.phase != phaseIntro {
		return m, nil
	}
	m.introFrame++
	return m, introTick()
}

// endIntro switches to the browser and primes the detail panel.
func (m model) endIntro() tea.Model {
	m.phase = phaseBrowse
	m.lastDetailKey = ""
	m.refreshDetail()
	return m
}

// handleIntroKey keeps the splash animating until the user presses Enter, which
// dissolves it into the browser. Quit keys still quit; other keys are ignored.
func (m model) handleIntroKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", keyCtrlC, keyEsc:
		return m, tea.Quit
	case keyEnter:
		return m.endIntro(), nil
	default:
		return m, nil
	}
}

// ---- config switcher (mirrors `config use`) ----

// openConfigPicker loads the saved configs and opens the switcher modal.
func (m model) openConfigPicker() tea.Model {
	names, err := config.ListConfigs()
	m.cfgNames = names
	m.cfgErr = err
	m.cfgCur = indexOf(names, m.cfgName)
	m.cfgPickerOpen = true
	return m
}

// handlePickerKey drives the open config switcher.
func (m model) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		return m, tea.Quit
	case keyEsc, "c", "q":
		m.cfgPickerOpen = false
		m.cfgErr = nil
		return m, nil
	case "up", "k":
		m.cfgCur = clamp(m.cfgCur-1, 0, len(m.cfgNames)-1)
		return m, nil
	case "down", "j":
		m.cfgCur = clamp(m.cfgCur+1, 0, len(m.cfgNames)-1)
		return m, nil
	case keyEnter:
		if m.cfgCur >= 0 && m.cfgCur < len(m.cfgNames) {
			return m.applyConfig(m.cfgNames[m.cfgCur])
		}
		return m, nil
	}
	return m, nil
}

// applyConfig switches the active config: it persists the choice (like
// `config use`), rebuilds the API client, and reloads everything.
func (m model) applyConfig(name string) (tea.Model, tea.Cmd) {
	if name == m.cfgName {
		m.cfgPickerOpen = false
		return m, nil
	}
	cfg, err := config.LoadConfig(name)
	if err != nil {
		m.cfgErr = fmt.Errorf("load %q: %w", name, err)
		return m, nil
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" {
		m.cfgErr = fmt.Errorf("config %q has no credentials", name)
		return m, nil
	}
	if err := config.SetCurrentConfigName(name); err != nil {
		m.cfgErr = err
		return m, nil
	}
	m.client = client.New(cfg.BaseURL, cfg.APIKey)
	m.cfgName = name
	m.cfgPickerOpen = false
	m.cfgErr = nil
	return m.reload()
}

func indexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return 0
}

// ---- update ----

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.applyLayout()
		m.lastDetailKey = ""
		m.refreshDetail()
		return m, nil

	case introTickMsg:
		return m.advanceIntro()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	default:
		return m.handleDataMsg(msg)
	}
}

// handleDataMsg routes the asynchronous load results (list pages, detail
// fetches and image previews) to their handlers. It is split out of Update to
// keep that dispatcher small.
func (m model) handleDataMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pageResult[api.SpaceListItem]:
		return m.handleSpacesPage(msg)
	case pageResult[api.CaseListItem]:
		return m.handleCaseListPage(msg)
	case pageResult[api.File]:
		return m.handleFilesPage(msg)
	case pageResult[api.NoteListItem]:
		return m.handleNotesPage(msg)
	case usersLoadedMsg:
		return m.handleUsersLoadedMsg(msg)
	case caseDetailMsg:
		return m.handleCaseDetailMsg(msg)
	case noteDetailMsg:
		return m.handleNoteDetailMsg(msg)
	case imageLoadedMsg:
		return m.handleImageLoadedMsg(msg)
	case fileTextMsg:
		return m.handleFileTextMsg(msg)
	}
	return m, nil
}

// handleUsersLoadedMsg records a loaded page of users, paging through the rest
// if more remain, and refreshes the detail pane once all pages are in.
func (m model) handleUsersLoadedMsg(msg usersLoadedMsg) (tea.Model, tea.Cmd) {
	more := msg.hasMore && msg.next != ""
	m.logFinishNote(msg.reqID, msg.err, pageNote(msg.count, more))
	if msg.gen != m.detailGen || msg.err != nil {
		return m, nil
	}
	if more {
		return m, m.issueUsers(msg.next, msg.page+1, msg.users)
	}
	m.users = make(map[string]api.User, len(msg.users))
	for _, user := range msg.users {
		m.users[user.Id.String()] = user
	}
	m.lastDetailKey = ""
	m.refreshDetail()
	return m, nil
}

// handleFileTextMsg records a loaded text/eml file body (or its error) and
// repaints the detail pane.
func (m model) handleFileTextMsg(msg fileTextMsg) (tea.Model, tea.Cmd) {
	delete(m.fileTextLoading, msg.id)
	m.logFinish(msg.reqID, msg.err)
	if msg.gen != m.detailGen {
		return m, nil // superseded by a reload / config switch
	}
	if msg.err != nil {
		m.fileTextErr[msg.id] = msg.err
	} else {
		m.fileText[msg.id] = msg.tc
	}
	m.lastDetailKey = ""
	m.refreshDetail()
	return m, nil
}

// handleImageLoadedMsg records a downloaded image and, if the user is still
// waiting to view it, launches the viewer.
func (m model) handleImageLoadedMsg(msg imageLoadedMsg) (tea.Model, tea.Cmd) {
	delete(m.imgLoading, msg.id)
	m.logFinish(msg.reqID, msg.err)
	if msg.gen != m.detailGen {
		return m, nil // superseded by a reload / config switch
	}
	if msg.err != nil {
		m.imgErr[msg.id] = msg.err
		if m.viewFile != nil && m.viewFile.Id.String() == msg.id {
			m.viewFile = nil
		}
		m.lastDetailKey = ""
		m.refreshDetail()
		return m, nil
	}
	m.imgCache[msg.id] = msg.img
	m.lastDetailKey = ""
	m.refreshDetail()
	if m.viewFile != nil && m.viewFile.Id.String() == msg.id {
		f := *m.viewFile
		m.viewFile = nil
		return m, m.execImage(f, msg.img)
	}
	return m, nil
}

// handleCaseListPage routes a case-shaped page to the browse or case screen,
// since cases and sub-cases share the same item type.
func (m model) handleCaseListPage(msg pageResult[api.CaseListItem]) (tea.Model, tea.Cmd) {
	if msg.kind == kindSubcases {
		return m.handleSubcasesPage(msg)
	}
	return m.handleCasesPage(msg)
}

func (m model) handleSpacesPage(msg pageResult[api.SpaceListItem]) (tea.Model, tea.Cmd) {
	m.logFinishNote(msg.reqID, msg.err, pageNote(len(msg.items), msg.hasMore))
	if msg.gen != m.sp.gen {
		return m, nil // superseded
	}
	m.sp.loading, m.sp.loadingMore = false, false
	if msg.err != nil {
		return m.recordErr(msg.err), nil
	}
	m.err = nil
	m.sp.applyPage(msg.items, msg.cursor, msg.hasMore, msg.appendPage)
	var cmd tea.Cmd
	if !msg.appendPage {
		cmd = m.onSpaceChange()
	}
	m.refreshDetail()
	return m, cmd
}

func (m model) handleCasesPage(msg pageResult[api.CaseListItem]) (tea.Model, tea.Cmd) {
	m.logFinishNote(msg.reqID, msg.err, pageNote(len(msg.items), msg.hasMore))
	if msg.gen != m.cs.gen || msg.parentID != m.cs.parentID {
		return m, nil // superseded or for another space
	}
	m.cs.loading, m.cs.loadingMore = false, false
	if msg.err != nil {
		return m.recordErr(msg.err), nil
	}
	m.err = nil
	wasEmpty := m.cs.len() == 0
	m.cs.applyPage(msg.items, msg.cursor, msg.hasMore, msg.appendPage)
	var cmds []tea.Cmd
	// Only the browse screen previews cases; never stomp case-screen children.
	if m.phase == phaseBrowse && (!msg.appendPage || (wasEmpty && m.cs.len() > 0)) {
		cmds = append(cmds, m.onBrowseCaseChange())
	}
	m.refreshDetail()
	return m, tea.Batch(cmds...)
}

func (m model) handleSubcasesPage(msg pageResult[api.CaseListItem]) (tea.Model, tea.Cmd) {
	m.logFinishNote(msg.reqID, msg.err, pageNote(len(msg.items), msg.hasMore))
	if msg.gen != m.scs.gen || msg.parentID != m.scs.parentID {
		return m, nil // superseded or for another case
	}
	m.scs.loading, m.scs.loadingMore = false, false
	if msg.err != nil {
		return m.recordErr(msg.err), nil
	}
	m.err = nil
	m.scs.applyPage(msg.items, msg.cursor, msg.hasMore, msg.appendPage)
	var cmds []tea.Cmd
	if !msg.appendPage && m.detailSource == focusSubcases {
		cmds = append(cmds, m.previewLoad(focusSubcases))
	}
	m.refreshDetail()
	return m, tea.Batch(cmds...)
}

func (m model) handleFilesPage(msg pageResult[api.File]) (tea.Model, tea.Cmd) {
	m.logFinishNote(msg.reqID, msg.err, pageNote(len(msg.items), msg.hasMore))
	if msg.gen != m.fl.gen || msg.parentID != m.fl.parentID {
		return m, nil // superseded or for another case
	}
	m.fl.loading, m.fl.loadingMore = false, false
	if msg.err != nil {
		return m.recordErr(msg.err), nil
	}
	m.err = nil
	m.fl.applyPage(msg.items, msg.cursor, msg.hasMore, msg.appendPage)
	var cmd tea.Cmd
	if !msg.appendPage && m.detailSource == focusFiles {
		cmd = m.previewLoad(focusFiles)
	}
	m.refreshDetail()
	return m, cmd
}

func (m model) handleNotesPage(msg pageResult[api.NoteListItem]) (tea.Model, tea.Cmd) {
	m.logFinishNote(msg.reqID, msg.err, pageNote(len(msg.items), msg.hasMore))
	if msg.gen != m.nt.gen || msg.parentID != m.nt.parentID {
		return m, nil // superseded or for another case
	}
	m.nt.loading, m.nt.loadingMore = false, false
	if msg.err != nil {
		return m.recordErr(msg.err), nil
	}
	m.err = nil
	m.nt.applyPage(msg.items, msg.cursor, msg.hasMore, msg.appendPage)
	var cmd tea.Cmd
	if !msg.appendPage && m.detailSource == focusNotes {
		cmd = m.previewLoad(focusNotes)
	}
	m.refreshDetail()
	return m, cmd
}

// recordErr stores a non-cancellation error for display.
func (m model) recordErr(err error) tea.Model {
	if !errors.Is(err, context.Canceled) {
		m.err = err
	}
	return m
}

func (m model) handleCaseDetailMsg(msg caseDetailMsg) (tea.Model, tea.Cmd) {
	m.logFinish(msg.reqID, msg.err)
	if msg.gen != m.detailGen {
		return m, nil // from before a reload/config switch
	}
	delete(m.caseDetailLoading, msg.id)
	if msg.err == nil {
		m.caseDetail[msg.id] = msg.c
	}
	if m.detailShowsCase(msg.id) {
		m.refreshDetail()
	}
	return m, nil
}

func (m model) handleNoteDetailMsg(msg noteDetailMsg) (tea.Model, tea.Cmd) {
	m.logFinish(msg.reqID, msg.err)
	if msg.gen != m.detailGen {
		return m, nil
	}
	delete(m.noteDetailLoading, msg.id)
	if msg.err == nil {
		m.noteDetail[msg.id] = msg.n
	}
	if m.detailSource == focusNotes && m.curNoteID() == msg.id {
		m.refreshDetail()
	}
	return m, nil
}

// detailShowsCase reports whether the detail pane currently renders case id.
func (m *model) detailShowsCase(id string) bool {
	switch m.effectiveDetailSource() {
	case focusCurrentCase:
		return m.currentCaseID() == id
	case focusCases:
		return m.curCaseID() == id
	case focusSubcases:
		return m.curSubcaseID() == id
	default:
		return false
	}
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.phase == phaseIntro {
		return m.handleIntroKey(msg)
	}
	if m.cfgPickerOpen {
		return m.handlePickerKey(msg)
	}
	switch msg.String() {
	case "q", keyCtrlC:
		return m, tea.Quit
	case keyEsc:
		return m.escapePressed()
	case keyEnter:
		return m.enterPressed()
	}
	if mdl, cmd, ok := m.handleActionKey(msg); ok {
		return mdl, cmd
	}
	return m.handleNavKey(msg)
}

// handleActionKey handles the global action keys (config, filter, toggles, file
// actions, reload). The bool reports whether the key was consumed.
func (m model) handleActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	switch msg.String() {
	case "c":
		return m.openConfigPicker(), nil, true
	case "f":
		mdl, cmd := m.cycleStatus()
		return mdl, cmd, true
	case "m":
		m.rawMode = !m.rawMode
		m.lastDetailKey = ""
		m.refreshDetail()
		return m, nil, true
	case "d":
		m.debug = !m.debug
		return m, nil, true
	case "v":
		mdl, cmd := m.viewFocusedImage()
		return mdl, cmd, true
	case "o":
		return m.openFocusedFile(), nil, true
	case "r":
		mdl, cmd := m.reload()
		return mdl, cmd, true
	}
	return m, nil, false
}

// viewFocusedImage previews the focused file if it is a viewable image.
func (m model) viewFocusedImage() (tea.Model, tea.Cmd) {
	if f, ok := m.focusedImageFile(); ok {
		return m.viewImage(f)
	}
	return m, nil
}

// openFocusedFile opens the selected file's download URL in the browser.
func (m model) openFocusedFile() tea.Model {
	f, ok := m.focusedFile()
	if !ok || f.DownloadUrl == "" {
		return m
	}
	if err := openBrowser(f.DownloadUrl); err != nil {
		return m.recordErr(err)
	}
	return m
}

// focusedFile returns the selected file when the files pane (or the detail pane
// sourced from it) is the active target on the case screen.
func (m model) focusedFile() (api.File, bool) {
	onFiles := m.focus == focusFiles || (m.focus == focusDetail && m.detailSource == focusFiles)
	if m.phase != phaseCase || !onFiles {
		return api.File{}, false
	}
	return m.fl.current()
}

// focusedImageFile returns the focused file when it is a previewable image on a
// graphics-capable terminal.
func (m model) focusedImageFile() (api.File, bool) {
	f, ok := m.focusedFile()
	if !ok || !imageSupported() || !isImageMime(f.MimeType) {
		return api.File{}, false
	}
	return f, true
}

// openBrowser opens rawURL in the user's default browser.
func openBrowser(rawURL string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return exec.Command(name, append(args, rawURL)...).Start()
}

// enterPressed opens (browse) or drills into (case) the selected case.
func (m model) enterPressed() (tea.Model, tea.Cmd) {
	switch m.phase {
	case phaseBrowse:
		if m.focus == focusCases {
			if c, ok := m.cs.current(); ok {
				return m.openCase(c)
			}
		}
	case phaseCase:
		// Sub-case: drill deeper into the case tree.
		if c, ok := m.scs.current(); ok && m.previewingSubcase() {
			return m.drillInto(c)
		}
		// File or note: drop into the bottom pane to read and scroll it.
		if m.focus == focusFiles || m.focus == focusNotes {
			m.setFocus(focusDetail)
			return m, nil
		}
		// Already reading an image in the bottom pane: open the full-screen viewer.
		if f, ok := m.focusedImageFile(); ok {
			return m.viewImage(f)
		}
	}
	return m, nil
}

// previewingSubcase is true when a sub-case is the active focus/preview target.
func (m model) previewingSubcase() bool {
	return m.focus == focusSubcases ||
		(m.focus == focusDetail && m.detailSource == focusSubcases)
}

// escapePressed walks back up the case tree, or quits from the browse screen.
func (m model) escapePressed() (tea.Model, tea.Cmd) {
	if m.phase != phaseCase {
		return m, tea.Quit
	}
	// Reading a file or note in the bottom pane: step back to its list pane.
	if m.focus == focusDetail && (m.detailSource == focusFiles || m.detailSource == focusNotes) {
		m.setFocus(m.detailSource)
		return m, nil
	}
	// Cancel child loads for the current case before changing context.
	m.scs.reset("")
	m.fl.reset("")
	m.nt.reset("")
	if len(m.caseStack) > 1 {
		m.caseStack = m.caseStack[:len(m.caseStack)-1]
		m.focus = focusSubcases
		return m, m.onCurrentCaseChange()
	}
	m.caseStack = nil
	m.phase = phaseBrowse
	m.focus = focusCases
	m.detailSource = focusCases
	m.lastDetailKey = ""
	cmd := m.onBrowseCaseChange()
	m.refreshDetail()
	return m, cmd
}

// handleNavKey handles pane focus and cursor movement keys.
func (m model) handleNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "right", "l":
		m.cycleFocus(1)
		cmd := m.focusLoadCmd()
		m.refreshDetail()
		return m, cmd
	case "shift+tab", "left", "h":
		m.cycleFocus(-1)
		cmd := m.focusLoadCmd()
		m.refreshDetail()
		return m, cmd
	case "up", "k":
		return m.moveCursor(-1)
	case "down", "j":
		return m.moveCursor(1)
	case "g", "home":
		return m.jumpCursor(true)
	case "G", "end":
		return m.jumpCursor(false)
	}
	return m, nil
}

// setFocus changes the focused pane. Focusing a list pane points the detail
// pane at it; focusing the detail pane leaves the content as-is for scrolling.
func (m *model) setFocus(f focus) {
	m.focus = f
	if f != focusDetail {
		m.detailSource = f
	}
}

// focusLoadCmd loads detail for the selection of the newly focused pane.
func (m *model) focusLoadCmd() tea.Cmd {
	return m.previewLoad(m.focus)
}

// cycleStatus advances the status filter and refetches the affected case lists
// from the top with the new server-side `status` param. Any loaded case and
// sub-case pane is reloaded so both stay consistent across the browse/case
// screens; their previews are refreshed when the new first page arrives.
func (m model) cycleStatus() (tea.Model, tea.Cmd) {
	m.statusFilter = nextStatus(m.statusFilter)
	var cmds []tea.Cmd
	if m.cs.parentID != "" {
		cmds = append(cmds, m.issueCases("", false))
	}
	if m.scs.parentID != "" {
		cmds = append(cmds, m.issueSubcases("", false))
	}
	m.lastDetailKey = ""
	m.refreshDetail()
	return m, tea.Batch(cmds...)
}

// moveCursor moves the selection in the focused pane (loading more if needed),
// or scrolls the detail panel.
func (m model) moveCursor(delta int) (tea.Model, tea.Cmd) {
	if m.focus == focusDetail {
		m.scrollDetail(delta)
		return m, nil
	}
	cmd := m.moveFocusedList(delta)
	m.syncOffsets(m.layout())
	m.refreshDetail()
	return m, cmd
}

// moveFocusedList moves the cursor in the focused list pane and returns the
// follow-up command (lazy-load + detail preview) when the selection changed.
func (m *model) moveFocusedList(delta int) tea.Cmd {
	switch m.focus {
	case focusSpaces:
		if m.sp.move(delta) {
			return tea.Batch(m.maybeMoreSpaces(), m.onSpaceChange())
		}
	case focusCases:
		if m.cs.move(delta) {
			return tea.Batch(m.maybeMoreCases(), m.previewFocused())
		}
	case focusSubcases:
		if m.scs.move(delta) {
			return tea.Batch(m.maybeMoreSubcases(), m.previewFocused())
		}
	case focusFiles:
		if m.fl.move(delta) {
			return tea.Batch(m.maybeMoreFiles(), m.previewFocused())
		}
	case focusNotes:
		if m.nt.move(delta) {
			return tea.Batch(m.maybeMoreNotes(), m.previewFocused())
		}
	}
	return nil
}

// scrollDetail scrolls the detail viewport one line in the given direction.
func (m *model) scrollDetail(delta int) {
	if delta < 0 {
		m.vp.ScrollUp(1)
	} else {
		m.vp.ScrollDown(1)
	}
}

func (m model) jumpCursor(toStart bool) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.focus {
	case focusSpaces:
		m.sp.jump(toStart)
		cmd = tea.Batch(m.maybeMoreSpaces(), m.onSpaceChange())
	case focusCases:
		m.cs.jump(toStart)
		cmd = tea.Batch(m.maybeMoreCases(), m.previewFocused())
	case focusSubcases:
		m.scs.jump(toStart)
		cmd = tea.Batch(m.maybeMoreSubcases(), m.previewFocused())
	case focusFiles:
		m.fl.jump(toStart)
		cmd = tea.Batch(m.maybeMoreFiles(), m.previewFocused())
	case focusNotes:
		m.nt.jump(toStart)
		cmd = tea.Batch(m.maybeMoreNotes(), m.previewFocused())
	case focusDetail:
		if toStart {
			m.vp.GotoTop()
		} else {
			m.vp.GotoBottom()
		}
		return m, nil
	}
	m.syncOffsets(m.layout())
	m.refreshDetail()
	return m, cmd
}

func (m *model) maybeMoreSpaces() tea.Cmd {
	if m.sp.needMore() {
		return m.issueSpaces(m.sp.cursor, true)
	}
	return nil
}

func (m *model) maybeMoreCases() tea.Cmd {
	if m.cs.needMore() {
		return m.issueCases(m.cs.cursor, true)
	}
	return nil
}

func (m *model) maybeMoreSubcases() tea.Cmd {
	if m.scs.needMore() {
		return m.issueSubcases(m.scs.cursor, true)
	}
	return nil
}

func (m *model) maybeMoreFiles() tea.Cmd {
	if m.fl.needMore() {
		return m.issueFiles(m.fl.cursor, true)
	}
	return nil
}

func (m *model) maybeMoreNotes() tea.Cmd {
	if m.nt.needMore() {
		return m.issueNotes(m.nt.cursor, true)
	}
	return nil
}

func (m model) reload() (tea.Model, tea.Cmd) {
	m.detailGen++
	m.caseDetail = map[string]*api.Case{}
	m.noteDetail = map[string]*api.Note{}
	m.caseDetailLoading = map[string]bool{}
	m.noteDetailLoading = map[string]bool{}
	m.users = map[string]api.User{}
	m.imgCache = map[string]imagePrepared{}
	m.imgLoading = map[string]bool{}
	m.imgErr = map[string]error{}
	m.viewFile = nil
	m.fileText = map[string]textContent{}
	m.fileTextLoading = map[string]bool{}
	m.fileTextErr = map[string]error{}
	m.sp.reset("")
	m.cs.reset("")
	m.scs.reset("")
	m.fl.reset("")
	m.nt.reset("")
	m.caseStack = nil
	m.phase = phaseBrowse
	m.focus = focusSpaces
	m.detailSource = focusSpaces
	m.err = nil
	m.lastDetailKey = ""
	return m, tea.Batch(m.spin.Tick, m.issueSpaces("", false), m.issueUsers("", 1, nil))
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
