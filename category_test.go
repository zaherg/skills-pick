package main

import (
	"maps"
	"strings"
	"testing"
)

func newCategoryTestTUI() *tui {
	state := &tui{
		catalog: &Catalog{Categories: []Category{
			{Name: "Engineering", Skills: []Skill{
				{Name: "go-tool", Source: "owner/tools"},
				{Name: "go-test", Source: "owner/tests"},
				{Name: "rust-tool", Source: "owner/tools"},
				{Name: "go-installed", Source: "owner/tools"},
			}},
			{Name: "Empty"},
			{Name: "Installed", Skills: []Skill{{Name: "go-installed"}}},
			{Name: "Design", Skills: []Skill{{Name: "design-tool", Source: "owner/design"}}},
		}},
		selected:  map[string]bool{"design-tool": true},
		installed: map[string]bool{"go-installed": true},
		termW:     80,
		termH:     24,
	}
	state.buildItems()
	return state
}

func TestTUICategoryToggle(t *testing.T) {
	state := newCategoryTestTUI()
	for _, step := range []struct {
		keys string
		want map[string]bool
	}{
		{" ", map[string]bool{"go-tool": true, "go-test": true, "rust-tool": true, "design-tool": true}},
		{"j ", map[string]bool{"go-tool": false, "go-test": true, "rust-tool": true, "design-tool": true}},
		{"k ", map[string]bool{"go-tool": true, "go-test": true, "rust-tool": true, "design-tool": true}},
		{" ", map[string]bool{"go-tool": false, "go-test": false, "rust-tool": false, "design-tool": true}},
	} {
		captureTUIOutput(t, func() {
			if !state.handleInput([]byte(step.keys)) {
				t.Fatalf("keys %q exited the picker", step.keys)
			}
		})
		if !maps.Equal(state.selected, step.want) {
			t.Fatalf("keys %q: selected = %v, want %v", step.keys, state.selected, step.want)
		}
	}
}

func TestTUICategoryToggleRespectsFilter(t *testing.T) {
	for _, mode := range []tuiState{stateNormal, stateFilter} {
		state := newCategoryTestTUI()
		state.filter = "GO"
		state.state = mode
		state.selected["rust-tool"] = true
		state.buildItems()
		for _, selectAll := range []bool{true, false} {
			captureTUIOutput(t, func() { state.handleInput([]byte(" ")) })
			want := map[string]bool{"go-tool": selectAll, "go-test": selectAll, "rust-tool": true, "design-tool": true}
			if !maps.Equal(state.selected, want) || state.filter != "GO" || state.state != mode {
				t.Fatalf("mode %v: selected = %v, filter = %q, state = %v; want %v with unchanged filter/state", mode, state.selected, state.filter, state.state, want)
			}
		}
	}
}

func TestTUICategoryNavigation(t *testing.T) {
	state := newCategoryTestTUI()
	for _, step := range []struct {
		key    string
		cursor int
		name   string
	}{
		{"\x1b[A", 5, "design-tool"},
		{"\x1b[B", 0, "Engineering"},
		{"j", 1, "go-tool"},
		{"k", 0, "Engineering"},
		{"\x1b[F", 5, "design-tool"},
		{"k", 4, "Design"},
		{"\x1b[H", 0, "Engineering"},
	} {
		captureTUIOutput(t, func() { state.handleInput([]byte(step.key)) })
		if state.cursor != step.cursor || state.items[state.cursor].name != step.name {
			t.Fatalf("key %q: cursor = %d (%s), want %d (%s)", step.key, state.cursor, state.items[state.cursor].name, step.cursor, step.name)
		}
	}
}

func TestTUICategoryRenderSelectionState(t *testing.T) {
	state := newCategoryTestTUI()
	for _, step := range []struct {
		keys string
		want string
	}{
		{"", "[ ] Engineering (0/3)"},
		{" ", "[✓] Engineering (3/3)"},
		{"j ", "[-] Engineering (2/3)"},
		{"k ", "[✓] Engineering (3/3)"},
		{" ", "[ ] Engineering (0/3)"},
	} {
		captureTUIOutput(t, func() { state.handleInput([]byte(step.keys)) })
		output := captureTUIOutput(t, state.render)
		if !strings.Contains(output, step.want) {
			t.Fatalf("keys %q: output missing %q: %q", step.keys, step.want, output)
		}
		if state.cursor == 0 && !strings.Contains(output, "\x1b[7m"+step.want) {
			t.Fatalf("category cursor is not highlighted: %q", output)
		}
		for _, hidden := range []string{"Empty", "Installed", "go-installed"} {
			if strings.Contains(output, hidden) {
				t.Fatalf("output contains hidden category or skill %q: %q", hidden, output)
			}
		}
	}
}

func TestTUICategoryRenderScrollsToHeader(t *testing.T) {
	state := newCategoryTestTUI()
	state.termH = 6 // Only one item row fits below the heading.
	state.cursor = 4
	output := captureTUIOutput(t, state.render)
	if !strings.Contains(output, "\x1b[7m[✓] Design (1/1)") {
		t.Fatalf("viewport did not follow category cursor: %q", output)
	}
	if rows := len(strings.Split(output, "\r\n")); rows > state.termH {
		t.Fatalf("rendered %d rows in a %d-row terminal", rows, state.termH)
	}
}
