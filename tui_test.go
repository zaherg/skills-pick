package main

import (
	"fmt"
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
	state := newInputTestTUI()
	output := captureTUIOutput(t, func() { state.handleFilterInput([]byte{13}) })
	for _, want := range []string{"Filter: go (esc to clear)\x1b[0m\r\n", "1 selected, enter to install"} {
		if !strings.Contains(output, want) {
			t.Errorf("rendered output missing %q: %q", want, output)
		}
	}
}

func TestTUIInputBatches(t *testing.T) {
	for _, tc := range []struct {
		name     string
		chunks   []string
		normal   bool
		filter   string
		state    tuiState
		cursor   int
		selected map[string]bool
		quit     bool
	}{
		{name: "paste", chunks: []string{"-test"}, filter: "go-test", state: stateFilter},
		{name: "long paste", chunks: []string{"-query-longer-than-sixteen-bytes"}, filter: "go-query-longer-than-sixteen-bytes", state: stateFilter},
		{name: "edit pasted text", chunks: []string{"-tesx\x7ft"}, filter: "go-test", state: stateFilter},
		{name: "start search", normal: true, chunks: []string{"/go-test"}, filter: "go-test", state: stateFilter},
		{name: "navigate and select", chunks: []string{"\x1b[B "}, filter: "go", state: stateFilter, cursor: 1, selected: map[string]bool{"go-test": true}},
		{name: "home and end", chunks: []string{"\x1b[F \x1b[H "}, filter: "go", state: stateFilter, selected: map[string]bool{"go-test": true, "go-tool": true}},
		{name: "toggle twice", chunks: []string{"  "}, filter: "go", state: stateFilter, selected: map[string]bool{"go-tool": false}},
		{name: "apply and select", chunks: []string{"-test\r "}, filter: "go-test", state: stateNormal, selected: map[string]bool{"go-test": true}},
		{name: "apply and restart", chunks: []string{"\r/go-test"}, filter: "go-test", state: stateFilter},
		{name: "clear and restart", chunks: []string{"\x1b/go-test"}, filter: "go-test", state: stateFilter},
		{name: "quit during search", chunks: []string{"-\x03ignored"}, filter: "go-", state: stateFilter, quit: true},
		{name: "quit after applying", chunks: []string{"\r\x03/ignored"}, filter: "go", state: stateNormal, quit: true},
		{name: "q after applying", chunks: []string{"\rq/ignored"}, filter: "go", state: stateNormal, quit: true},
		{name: "ignore unknown CSI", chunks: []string{"\x1b[3~-test"}, filter: "go-test", state: stateFilter},
		{name: "ignore unknown SS3", chunks: []string{"\x1bOP-test"}, filter: "go-test", state: stateFilter},
		{name: "split CSI", chunks: []string{"\x1b[", "B "}, filter: "go", state: stateFilter, cursor: 1, selected: map[string]bool{"go-test": true}},
		{name: "split parameterized CSI", chunks: []string{"\x1b[1;", "5D", "-test"}, filter: "go-test", state: stateFilter},
		{name: "quit during incomplete CSI", chunks: []string{"\x1b[3", "\x03ignored"}, filter: "go", state: stateFilter, quit: true},
		{name: "empty batch", chunks: []string{""}, filter: "go", state: stateFilter},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := newInputTestTUI()
			if tc.normal {
				state.state = stateNormal
			}
			keepRunning := true
			captureTUIOutput(t, func() {
				for _, chunk := range tc.chunks {
					keepRunning = state.handleInput([]byte(chunk))
					if !keepRunning {
						break
					}
				}
			})
			if keepRunning == tc.quit || state.filter != tc.filter || state.state != tc.state || state.cursor != tc.cursor {
				t.Fatalf("running = %v, filter = %q, state = %v, cursor = %d; want %v, %q, %v, %d", keepRunning, state.filter, state.state, state.cursor, !tc.quit, tc.filter, tc.state, tc.cursor)
			}
			wantSelected := map[string]bool{"ce-plan": true}
			maps.Copy(wantSelected, tc.selected)
			if !maps.Equal(state.selected, wantSelected) {
				t.Fatalf("selected = %v, want %v", state.selected, wantSelected)
			}
		})
	}
}

func TestTUILoopReadsBatchesAndStops(t *testing.T) {
	for _, quit := range []bool{false, true} {
		t.Run(fmt.Sprintf("ctrl-c=%t", quit), func(t *testing.T) {
			input, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { input.Close() })
			t.Cleanup(func() { writer.Close() })
			query := strings.Repeat("x", 40)
			keys := "/" + query
			if quit {
				keys += "\x03ignored"
			}
			if _, err := writer.WriteString(keys); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			original := os.Stdin
			os.Stdin = input
			t.Cleanup(func() { os.Stdin = original })
			state := newInputTestTUI()
			state.state = stateNormal
			captureTUIOutput(t, state.loop)
			if state.filter != query || state.state != stateFilter {
				t.Fatalf("filter = %q, state = %v; want %q, stateFilter", state.filter, state.state, query)
			}
		})
	}
}

func TestTUIRenderEmptyResultsFitViewport(t *testing.T) {
	for _, size := range []struct{ height, rows int }{{5, 5}, {6, 5}, {12, 11}, {24, 23}, {40, 39}} {
		for _, mode := range []tuiState{stateNormal, stateFilter} {
			t.Run(fmt.Sprintf("height=%d/state=%d", size.height, mode), func(t *testing.T) {
				state := newInputTestTUI()
				state.termH = size.height
				state.state = mode
				state.filter = "missing"
				state.buildItems()
				output := captureTUIOutput(t, state.render)
				rows := strings.Split(output, "\r\n")
				if len(rows) != size.rows || len(rows) > size.height {
					t.Fatalf("rendered %d rows in a %d-row terminal, want %d", len(rows), size.height, size.rows)
				}
				if !strings.Contains(rows[0], "skills-pick") || strings.Count(output, "no skills match filter") != 1 {
					t.Fatalf("missing header or empty-state message: %q", output)
				}
				footer := "1 selected, enter to install"
				if mode == stateFilter {
					footer = "Filter: missing"
				}
				if !strings.Contains(rows[len(rows)-1], footer) {
					t.Fatalf("last row = %q, want footer %q", rows[len(rows)-1], footer)
				}
			})
		}
	}
}

func captureTUIOutput(t *testing.T, render func()) string {
	t.Helper()
	output, err := os.CreateTemp(t.TempDir(), "render")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = output
	defer func() {
		os.Stdout = original
		output.Close()
	}()
	render()
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
