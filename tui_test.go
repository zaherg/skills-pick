package main

import (
	"maps"
	"os"
	"strings"
	"testing"
)

func newInputTestTUI() *tui {
	state := &tui{
		catalog: &Catalog{Categories: []Category{
			{Name: "Core", Skills: []Skill{{Name: "ce-plan"}}},
			{Name: "Engineering", Skills: []Skill{{Name: "go-tool"}, {Name: "go-test"}, {Name: "go-installed"}}},
			{Name: "Design", Skills: []Skill{{Name: "design-tool"}}},
		}},
		selected:  map[string]bool{"ce-plan": true},
		installed: map[string]bool{"go-installed": true},
		state:     stateFilter,
		filter:    "go",
		termW:     80,
		termH:     12,
	}
	state.buildItems()
	return state
}

func TestTUIFilterInputNavigatesAndToggles(t *testing.T) {
	state := newInputTestTUI()
	for _, step := range []struct {
		key      string
		cursor   int
		selected map[string]bool
	}{
		{" ", 0, map[string]bool{"ce-plan": true, "go-tool": true}},
		{" ", 0, map[string]bool{"ce-plan": true, "go-tool": false}},
		{"\x1b[B", 1, map[string]bool{"ce-plan": true, "go-tool": false}},
		{" ", 1, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
		{"\x1b[A", 0, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
		{"\x1b[A", 1, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
		{"\x1b[B", 0, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
		{"\x1b[F", 1, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
		{"\x1b[H", 0, map[string]bool{"ce-plan": true, "go-tool": false, "go-test": true}},
	} {
		if !state.handleFilterInput([]byte(step.key)) {
			t.Fatalf("key %q exited the picker", step.key)
		}
		if state.state != stateFilter || state.filter != "go" {
			t.Fatalf("key %q changed search: state = %v, filter = %q", step.key, state.state, state.filter)
		}
		if state.cursor != step.cursor || !maps.Equal(state.selected, step.selected) {
			t.Fatalf("key %q: cursor = %d, selected = %v; want %d, %v", step.key, state.cursor, state.selected, step.cursor, step.selected)
		}
	}
}

func TestTUIFilterInputWithNoMatches(t *testing.T) {
	state := newInputTestTUI()
	state.handleFilterInput([]byte("x"))
	for _, key := range []string{" ", "\x1b[B", "\x1b[A", "\x1b[H", "\x1b[F"} {
		if !state.handleFilterInput([]byte(key)) {
			t.Fatalf("key %q exited the picker", key)
		}
		if state.filter != "gox" || state.numSkills() != 0 || state.cursor != 0 || !maps.Equal(state.selected, map[string]bool{"ce-plan": true}) {
			t.Fatalf("key %q changed an empty search: %#v", key, state)
		}
	}
	state.handleFilterInput([]byte{127})
	if state.filter != "go" || state.numSkills() != 2 || state.cursor != 0 {
		t.Fatalf("backspace did not restore matching skills: %#v", state)
	}
}

func TestTUIFilterInputKeepsLettersAsQueryText(t *testing.T) {
	for _, key := range []string{"j", "k", "q"} {
		t.Run(key, func(t *testing.T) {
			state := newInputTestTUI()
			if !state.handleFilterInput([]byte(key)) || state.filter != "go"+key || state.state != stateFilter {
				t.Fatalf("letter %q was not handled as search text: %#v", key, state)
			}
		})
	}
}

func TestTUICancelFilterRestoresFullList(t *testing.T) {
	for _, filter := range []string{"go", "missing", ""} {
		for _, apply := range []bool{false, true} {
			name := filter + "/editing"
			if apply {
				name = filter + "/applied"
			}
			t.Run(name, func(t *testing.T) {
				state := newInputTestTUI()
				state.filter = filter
				state.buildItems()
				state.selected["go-test"] = true
				if apply {
					if !state.handleFilterInput([]byte{13}) || state.state != stateNormal || state.filter != filter {
						t.Fatalf("enter did not apply filter: %#v", state)
					}
				}
				var keepRunning bool
				if state.state == stateFilter {
					keepRunning = state.handleFilterInput([]byte{0x1b})
				} else {
					keepRunning = state.handleNormalInput([]byte{0x1b})
				}
				if wantRunning := !apply || filter != ""; keepRunning != wantRunning {
					t.Fatalf("escape returned %v, want %v", keepRunning, wantRunning)
				}
				assertFullInputTestList(t, state)
				if state.state != stateNormal || !maps.Equal(state.selected, map[string]bool{"ce-plan": true, "go-test": true}) {
					t.Fatalf("cancel changed selections or left search mode active: %#v", state)
				}
			})
		}
	}
}

func TestTUIRestartSearchRebuildsItems(t *testing.T) {
	for _, filter := range []string{"go", "missing"} {
		t.Run(filter, func(t *testing.T) {
			state := newInputTestTUI()
			state.filter = filter
			state.state = stateNormal
			state.buildItems()
			if !state.handleNormalInput([]byte("/")) || state.state != stateFilter {
				t.Fatal("slash did not enter search mode")
			}
			assertFullInputTestList(t, state)
			if !maps.Equal(state.selected, map[string]bool{"ce-plan": true}) {
				t.Fatalf("restarting search changed selections: %v", state.selected)
			}
		})
	}
}

func assertFullInputTestList(t *testing.T, state *tui) {
	t.Helper()
	var names []string
	for _, item := range state.items {
		names = append(names, item.name)
	}
	if got, want := strings.Join(names, ","), "Core,ce-plan,Engineering,go-tool,go-test,Design,design-tool"; got != want {
		t.Fatalf("items = %q, want %q", got, want)
	}
	if state.filter != "" || state.cursor < 0 || state.cursor >= state.numSkills() {
		t.Fatalf("invalid cleared search: filter = %q, cursor = %d", state.filter, state.cursor)
	}
}

func TestTUIRenderShowsAppliedFilter(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "render")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	t.Cleanup(func() {
		os.Stdout = original
		output.Close()
	})
	state := newInputTestTUI()
	state.handleFilterInput([]byte{13})
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Filter: go (esc to clear)\x1b[0m\r\n", "1 selected, enter to install"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("rendered output missing %q: %q", want, data)
		}
	}
}
