package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/interloom/cli/internal/api"
)

func testModel() model {
	return newModel(context.Background(), nil, "test")
}

func testKey(s string) tea.KeyPressMsg {
	switch s {
	case keyCtrlC:
		return tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	case keyEsc:
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case keyEnter:
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	default:
		r := []rune(s)
		if len(r) == 0 {
			return tea.KeyPressMsg(tea.Key{})
		}
		return tea.KeyPressMsg(tea.Key{Text: s, Code: r[0]})
	}
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func requireNoCmd(t *testing.T, label string, cmd tea.Cmd) {
	t.Helper()
	if cmd != nil {
		t.Fatalf("%s returned a command", label)
	}
}

func requireQuitCmd(t *testing.T, label string, cmd tea.Cmd) {
	t.Helper()
	if !isQuitCmd(cmd) {
		t.Fatalf("%s did not quit", label)
	}
}

func requireEqual[T comparable](t *testing.T, label string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
}

func requireTrue(t *testing.T, label string, got bool) {
	t.Helper()
	requireEqual(t, label, got, true)
}

func TestBrowseEscAndQDoNotQuit(t *testing.T) {
	m := testModel()
	m.phase = phaseBrowse

	got, cmd := m.handleKey(testKey("q"))
	requireNoCmd(t, "q on browse screen", cmd)
	requireBrowsePhase(t, "q", got.(model))

	got, cmd = m.handleKey(testKey(keyEsc))
	requireNoCmd(t, "esc on browse screen", cmd)
	requireBrowsePhase(t, "esc", got.(model))

	_, cmd = m.handleKey(testKey(keyCtrlC))
	requireQuitCmd(t, "ctrl+c on browse screen", cmd)
}

func requireBrowsePhase(t *testing.T, label string, m model) {
	t.Helper()
	requireEqual(t, label+" phase", m.phase, phaseBrowse)
}

func TestIntroOnlyCtrlCQuits(t *testing.T) {
	m := testModel()
	m.phase = phaseIntro

	_, cmd := m.handleIntroKey(testKey("q"))
	requireNoCmd(t, "q on intro screen", cmd)
	_, cmd = m.handleIntroKey(testKey(keyEsc))
	requireNoCmd(t, "esc on intro screen", cmd)
	_, cmd = m.handleIntroKey(testKey(keyCtrlC))
	requireQuitCmd(t, "ctrl+c on intro screen", cmd)
}

func TestPickerQDoesNotCancel(t *testing.T) {
	m := testModel()
	m.phase = phaseBrowse
	m.cfgPickerOpen = true

	got, cmd := m.handlePickerKey(testKey("q"))
	requireNoCmd(t, "q in config picker", cmd)
	requirePickerOpen(t, "q", got.(model), true)
	got, cmd = m.handlePickerKey(testKey(keyEsc))
	requireNoCmd(t, "esc in config picker", cmd)
	requirePickerOpen(t, "esc", got.(model), false)
	_, cmd = m.handlePickerKey(testKey(keyCtrlC))
	requireQuitCmd(t, "ctrl+c in config picker", cmd)
}

func requirePickerOpen(t *testing.T, label string, m model, want bool) {
	t.Helper()
	requireEqual(t, label+" picker open", m.cfgPickerOpen, want)
}

func TestStatusFilterShownInCasePaneTitlesNotHeader(t *testing.T) {
	m := testModel()
	m.width = 100
	m.statusFilter = api.Started

	header := ansi.Strip(m.renderHeader())
	requireTrue(t, "header omits filter label", !strings.Contains(header, "filter"))
	requireTrue(t, "header omits filter value", !strings.Contains(header, string(api.Started)))

	cases := ansi.Strip(m.renderPane(focusCases, 44, 8))
	requireTrue(t, "cases pane shows filter label", strings.Contains(cases, "filter"))
	requireTrue(t, "cases pane shows filter value", strings.Contains(cases, string(api.Started)))

	subcases := ansi.Strip(m.renderPane(focusSubcases, 44, 8))
	requireTrue(t, "sub-cases pane shows filter label", strings.Contains(subcases, "filter"))
	requireTrue(t, "sub-cases pane shows filter value", strings.Contains(subcases, string(api.Started)))
}

const (
	testRootTitle  = "root"
	testChildTitle = "child"
)

func TestTaskNavigationClearsStatusFilter(t *testing.T) {
	t.Run("open case", func(t *testing.T) {
		m := filteredBoundaryModel()
		mdl, cmd := m.openCase(api.CaseListItem{Title: "case"})
		got := mdl.(model)

		requireTrue(t, "open returned command", cmd != nil)
		requireFilterClearedAndBrowseCasesReloading(t, got)
		requireEqual(t, "phase", got.phase, phaseCase)
		requireEqual(t, "case stack length", len(got.caseStack), 1)
		requireTrue(t, "subcases loading", got.scs.loading)
	})

	t.Run("drill into sub-case", func(t *testing.T) {
		m := filteredBoundaryModel()
		m.phase = phaseCase
		m.caseStack = []api.CaseListItem{{Title: testRootTitle}}

		mdl, cmd := m.drillInto(api.CaseListItem{Title: testChildTitle})
		got := mdl.(model)

		requireTrue(t, "drill returned command", cmd != nil)
		requireFilterClearedAndBrowseCasesReloading(t, got)
		requireEqual(t, "case stack length", len(got.caseStack), 2)
		requireTrue(t, "subcases loading", got.scs.loading)
	})

	t.Run("escape to parent case", func(t *testing.T) {
		m := filteredBoundaryModel()
		m.phase = phaseCase
		m.caseStack = []api.CaseListItem{{Title: testRootTitle}, {Title: testChildTitle}}

		mdl, cmd := m.escapePressed()
		got := mdl.(model)

		requireTrue(t, "escape returned command", cmd != nil)
		requireFilterClearedAndBrowseCasesReloading(t, got)
		requireEqual(t, "phase", got.phase, phaseCase)
		requireEqual(t, "case stack length", len(got.caseStack), 1)
		requireTrue(t, "subcases loading", got.scs.loading)
	})

	t.Run("escape to browse", func(t *testing.T) {
		m := filteredBoundaryModel()
		m.phase = phaseCase
		m.caseStack = []api.CaseListItem{{Title: testRootTitle}}

		mdl, cmd := m.escapePressed()
		got := mdl.(model)

		requireTrue(t, "escape returned command", cmd != nil)
		requireFilterClearedAndBrowseCasesReloading(t, got)
		requireEqual(t, "phase", got.phase, phaseBrowse)
		requireEqual(t, "focus", got.focus, focusCases)
		requireEqual(t, "detail source", got.detailSource, focusCases)
	})
}

func filteredBoundaryModel() model {
	m := testModel()
	m.statusFilter = api.Blocked
	m.cs.parentID = "space-1"
	return m
}

func requireFilterClearedAndBrowseCasesReloading(t *testing.T, got model) {
	t.Helper()
	requireEqual(t, "status filter", got.statusFilter, api.CaseStatus(""))
	requireTrue(t, "browse cases reloading", got.cs.loading)
}

func seededReloadModel() (model, string) {
	m := testModel()
	id := api.CaseListItem{}.Id.String()
	m.phase = phaseCase
	m.focus = focusThread
	m.detailSource = focusThread
	m.rawMode = true
	m.debug = true
	m.statusFilter = api.Started
	m.cfgName = "prod"
	m.caseStack = []api.CaseListItem{{Title: "current"}}
	m.sp.items = []api.SpaceListItem{{Name: "space"}}
	m.sp.cur, m.sp.off = 1, 2
	m.cs.parentID = "space-1"
	m.cs.items = []api.CaseListItem{{Title: "browse"}}
	m.cs.cur, m.cs.off = 3, 4
	m.scs.parentID = id
	m.scs.items = []api.CaseListItem{{Title: "child"}}
	m.fl.parentID = id
	m.fl.items = []api.File{{Name: "file"}}
	m.nt.parentID = id
	m.nt.items = []api.NoteListItem{{Title: "note"}}
	m.th.parentID = "thread-1"
	m.attachmentsCur, m.attachmentsOff = 2, 1
	m.caseDetail[id] = &api.Case{Title: "old current"}
	m.caseDetail["other"] = &api.Case{Title: "other"}
	m.noteDetail["note"] = nil
	m.users["u"] = api.User{Name: "User"}
	return m, id
}

func TestReloadCurrentCasePreservesApplicationState(t *testing.T) {
	m, id := seededReloadModel()
	oldDetailGen := m.detailGen

	mdl, cmd := m.reloadForContext()
	requireTrue(t, "reloadCurrentCase returned load command", cmd != nil)
	got := mdl.(model)

	requireReloadKeptViewState(t, got, id)
	requireReloadKeptBrowseState(t, got)
	requireReloadInvalidatedCurrentCaseData(t, got, id, oldDetailGen)
	requireReloadStartedCurrentCaseLoads(t, got, id)
}

func requireReloadKeptViewState(t *testing.T, got model, id string) {
	t.Helper()
	requireEqual(t, "phase", got.phase, phaseCase)
	requireEqual(t, "focus", got.focus, focusThread)
	requireEqual(t, "detail source", got.detailSource, focusThread)
	requireEqual(t, "case stack length", len(got.caseStack), 1)
	requireEqual(t, "current case", got.currentCaseID(), id)
	requireTrue(t, "raw mode", got.rawMode)
	requireTrue(t, "debug", got.debug)
	requireEqual(t, "status filter", got.statusFilter, api.Started)
	requireEqual(t, "config name", got.cfgName, "prod")
}

func requireReloadKeptBrowseState(t *testing.T, got model) {
	t.Helper()
	requireEqual(t, "spaces length", got.sp.len(), 1)
	requireEqual(t, "spaces cursor", got.sp.cur, 1)
	requireEqual(t, "spaces offset", got.sp.off, 2)
	requireEqual(t, "cases length", got.cs.len(), 1)
	requireEqual(t, "cases parent", got.cs.parentID, "space-1")
	requireEqual(t, "cases cursor", got.cs.cur, 3)
	requireEqual(t, "cases offset", got.cs.off, 4)
	requireEqual(t, "users length", len(got.users), 1)
}

func requireReloadInvalidatedCurrentCaseData(t *testing.T, got model, id string, oldDetailGen int) {
	t.Helper()
	requireEqual(t, "detail generation", got.detailGen, oldDetailGen+1)
	requireTrue(t, "current case detail invalidated", got.caseDetail[id] == nil)
	requireTrue(t, "unrelated case detail preserved", got.caseDetail["other"] != nil)
	requireEqual(t, "note detail length", len(got.noteDetail), 0)
}

func requireReloadStartedCurrentCaseLoads(t *testing.T, got model, id string) {
	t.Helper()
	requireEqual(t, "subcases parent", got.scs.parentID, id)
	requireEqual(t, "files parent", got.fl.parentID, id)
	requireEqual(t, "notes parent", got.nt.parentID, id)
	requireEqual(t, "subcases length", got.scs.len(), 0)
	requireEqual(t, "files length", got.fl.len(), 0)
	requireEqual(t, "notes length", got.nt.len(), 0)
	requireTrue(t, "subcases loading", got.scs.loading)
	requireTrue(t, "files loading", got.fl.loading)
	requireTrue(t, "notes loading", got.nt.loading)
	requireEqual(t, "thread parent", got.th.parentID, "")
	requireEqual(t, "thread length", got.th.len(), 0)
	requireEqual(t, "thread loading", got.th.loading, false)
	requireEqual(t, "attachment cursor", got.attachmentsCur, 0)
	requireEqual(t, "attachment offset", got.attachmentsOff, 0)
}
