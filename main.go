package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/term"
)

//go:embed catalog.json
var embeddedCatalog []byte

type Skill struct {
	Name        string `json:"name"`
	Source      string `json:"source"`
	Description string `json:"description"`
}

type Category struct {
	Name   string  `json:"name"`
	Skills []Skill `json:"skills"`
}

type Catalog struct {
	Version    int        `json:"version"`
	Categories []Category `json:"categories"`
}

var version = "dev"

const changelog = `0.3.2
  + update workflows to use the correct github repo

0.3.1
  + -a opencode flag added to npx skills add command (project-level needs agent)
  + bumped default agent to opencode for project installations

0.3.0
  + changelog subcommand: prints this log via 'skills-pick changelog'
  + --version flag: prints the current version and exits
  + update subcommand: runs 'npx --yes skills update' to refresh all installed skills
  + interactive mode now hides skills that are already installed in the current
    project or globally (~/.agents, ~/.claude, ~/.config/opencode and the
    project's .agents, .claude, .opencode skill directories)
  + verified npx 'skills add' takes only one source per invocation, so the
    installer continues to group by source and run one npx per source

0.2.0
  + add <source> <skill...> [-c category] [-d description]: register a skill
    in the local catalog (~/config/skills-pick/catalog.json)
  + interactive TUI: j/k to move, space to toggle, / to filter, enter to
    install, q to quit
  + direct install mode: pass skill names as positional args to install
    without entering the TUI
  + loadCatalog() prefers --catalog flag, then the user config file, then
    the embedded catalog baked into the binary

0.1.0
  + initial release: interactive picker, 'list' subcommand, direct install
  + embedded catalog (catalog.json) ships a curated set of skills across
    Engineering, Design, SEO & Marketing, and Tools categories
  + runInstall() groups selected skills by source and invokes
    'npx --yes skills add <source> -y --skill <name>' per source
`

var (
	helpFlag    bool
	versionFlag bool
	filterFlag  string
	catalogFlag string
)

func init() {
	flag.BoolVar(&helpFlag, "help", false, "show help")
	flag.BoolVar(&versionFlag, "version", false, "print version and exit")
	flag.StringVar(&filterFlag, "filter", "", "pre-apply a filter in interactive mode")
	flag.StringVar(&catalogFlag, "catalog", "", "use a custom catalog file")
}

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usageText()) }
	flag.Parse()

	if helpFlag {
		fmt.Print(usageText())
		return
	}

	if versionFlag {
		fmt.Print(versionOutput())
		return
	}

	cat := loadCatalog()
	args := flag.Args()

	if len(args) == 0 {
		interactive(cat)
		return
	}

	switch args[0] {
	case "help":
		fmt.Print(usageText())
	case "list":
		listSkills(cat)
	case "interactive":
		interactive(cat)
	case "add":
		cmdAdd(args[1:])
	case "update":
		cmdUpdate()
	case "changelog":
		fmt.Print(changelog)
	default:
		installDirect(cat, args)
	}
}

const usageTemplate = `skills-pick %s - interactive agent skill installer

Usage:
  skills-pick [command] [skill ...]

Commands:
  help              Show this help
  list              List available skills
  add <source> <skill> [more skills...] [-c category] [-d description]
                    Add one or more skills from a source to the catalog
  update            Update all installed skills via 'npx skills update'
  changelog         Show the changelog
  interactive       Interactive TUI selector (default)

Without arguments, starts the interactive TUI.
With skill names as arguments, installs them directly.

Examples:
  skills-pick                              Interactive mode
  skills-pick list                         List all skills
  skills-pick seo-audit ce-plan            Install specific skills
  skills-pick add zaherg/chorus consensus  Add a skill to the catalog
`

func versionOutput() string {
	return fmt.Sprintf("skills-pick %s\n", version)
}

func usageText() string {
	return fmt.Sprintf(usageTemplate, version)
}

func loadCatalog() *Catalog {
	cat := &Catalog{}
	data := embeddedCatalog

	override := catalogFlag
	if override == "" {
		home, _ := os.UserHomeDir()
		cfg := home + "/.config/skills-pick/catalog.json"
		if _, err := os.Stat(cfg); err == nil {
			override = cfg
		}
	}
	if override != "" {
		b, err := os.ReadFile(override)
		if err == nil {
			data = b
		}
	}

	if err := json.Unmarshal(data, cat); err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse catalog: %v\n", err)
		os.Exit(1)
	}
	if cat.Version < 1 {
		fmt.Fprintln(os.Stderr, "invalid catalog: missing version")
		os.Exit(1)
	}
	return cat
}

func listSkills(cat *Catalog) {
	maxW := 0
	for _, c := range cat.Categories {
		for _, s := range c.Skills {
			if n := len(s.Name); n > maxW {
				maxW = n
			}
		}
	}
	maxW += 2

	for _, c := range cat.Categories {
		fmt.Printf("\n\x1b[1m%s:\x1b[0m\n", c.Name)
		for _, s := range c.Skills {
			pad := maxW - len(s.Name)
			fmt.Printf("  \x1b[36m%s\x1b[0m%*s%s\n", s.Name, pad, "", s.Description)
		}
	}
	fmt.Println()
}

func installDirect(cat *Catalog, names []string) {
	lookup := buildLookup(cat)
	var selected []Skill
	var notFound []string
	for _, name := range names {
		s, ok := lookup[name]
		if !ok {
			notFound = append(notFound, name)
			continue
		}
		selected = append(selected, s)
	}
	if len(notFound) > 0 {
		fmt.Fprintf(os.Stderr, "unknown skills: %s\n\n", strings.Join(notFound, ", "))
		fmt.Fprintln(os.Stderr, "Run 'skills-pick list' to see available skills.")
		os.Exit(1)
	}
	runInstall(selected)
}

func buildLookup(cat *Catalog) map[string]Skill {
	m := make(map[string]Skill)
	for _, c := range cat.Categories {
		for _, s := range c.Skills {
			m[s.Name] = s
		}
	}
	return m
}

func runInstall(skills []Skill) {
	if len(skills) == 0 {
		return
	}
	bySource := make(map[string][]string)
	for _, s := range skills {
		bySource[s.Source] = append(bySource[s.Source], s.Name)
	}
	for source, names := range bySource {
		fmt.Printf("\n\x1b[1mInstalling from %s:\x1b[0m %s\n", source, strings.Join(names, ", "))
		args := []string{"--yes", "skills", "add", source, "-a", "opencode", "-y"}
		for _, n := range names {
			args = append(args, "--skill", n)
		}
		cmd := exec.Command("npx", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "\x1b[31mfailed to install from %s: %v\x1b[0m\n", source, err)
		}
	}
	fmt.Printf("\n\x1b[1mDone.\x1b[0m Installed:")
	for _, s := range skills {
		fmt.Printf(" %s", s.Name)
	}
	fmt.Println()
}

func catalogOverridePath() string {
	if catalogFlag != "" {
		return catalogFlag
	}
	home, _ := os.UserHomeDir()
	return home + "/.config/skills-pick/catalog.json"
}

func installedSkillNames() map[string]bool {
	set := make(map[string]bool)
	roots := []string{}

	if home, err := os.UserHomeDir(); err == nil {
		for _, p := range []string{".agents/skills", ".claude/skills", ".config/opencode/skills"} {
			roots = append(roots, filepath.Join(home, p))
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for _, p := range []string{".agents/skills", ".claude/skills", ".opencode/skills"} {
			roots = append(roots, filepath.Join(cwd, p))
		}
	}

	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				set[e.Name()] = true
			}
		}
	}
	return set
}

func cmdUpdate() {
	fmt.Println("\x1b[1mUpdating installed skills...\x1b[0m")
	cmd := exec.Command("npx", "--yes", "skills", "update")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\x1b[31mfailed to run skills update: %v\x1b[0m\n", err)
		os.Exit(1)
	}
}

func cmdAdd(args []string) {
	var category, desc string
	var positional []string

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--category" || args[i] == "-c":
			i++
			if i < len(args) {
				category = args[i]
			}
		case args[i] == "--desc" || args[i] == "-d":
			i++
			if i < len(args) {
				desc = args[i]
			}
		case strings.HasPrefix(args[i], "--category="):
			category = strings.TrimPrefix(args[i], "--category=")
		case strings.HasPrefix(args[i], "--desc="):
			desc = strings.TrimPrefix(args[i], "--desc=")
		default:
			positional = append(positional, args[i])
		}
	}
	if category == "" {
		category = "Tools"
	}
	if len(positional) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: skills-pick add <source> <skill> [skill ...] [-c category] [-d description]")
		os.Exit(1)
	}

	source := positional[0]
	skillNames := positional[1:]
	cat := loadCatalog()

	var target *Category
	for i := range cat.Categories {
		if cat.Categories[i].Name == category {
			target = &cat.Categories[i]
			break
		}
	}
	if target == nil {
		cat.Categories = append(cat.Categories, Category{Name: category})
		target = &cat.Categories[len(cat.Categories)-1]
	}

	existing := make(map[string]bool)
	for _, s := range target.Skills {
		existing[s.Name] = true
	}
	for _, name := range skillNames {
		if existing[name] {
			fmt.Fprintf(os.Stderr, "skill %q already exists in %q, skipping\n", name, category)
			continue
		}
		target.Skills = append(target.Skills, Skill{
			Name:        name,
			Source:      source,
			Description: desc,
		})
		existing[name] = true
		fmt.Printf("added %q to category %q\n", name, category)
	}

	path := catalogOverridePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create directory: %v\n", err)
		os.Exit(1)
	}
	data, err := json.MarshalIndent(cat, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to serialize catalog: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write catalog: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("catalog written to %s\n", path)
}

// --- Interactive TUI ---

type tuiState int

const (
	stateNormal tuiState = iota
	stateFilter
)

type visibleItem struct {
	isHeader bool
	name     string
	source   string
	desc     string
}

type tui struct {
	catalog   *Catalog
	items     []visibleItem // built from catalog for current filter
	cursor    int           // index into items (always a skill, never header)
	selected  map[string]bool
	installed map[string]bool
	state     tuiState
	filter    string

	termW, termH int
	oldState     *term.State
}

func interactive(cat *Catalog) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "skills-pick requires an interactive terminal")
		os.Exit(1)
	}

	t := &tui{
		catalog:   cat,
		selected:  make(map[string]bool),
		installed: installedSkillNames(),
		filter:    filterFlag,
	}

	var err error
	t.oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to enter raw mode: %v\n", err)
		os.Exit(1)
	}
	defer term.Restore(int(os.Stdin.Fd()), t.oldState)

	t.termW, t.termH, _ = term.GetSize(int(os.Stdin.Fd()))
	if t.termW < 20 {
		t.termW = 80
	}
	if t.termH < 5 {
		t.termH = 24
	}

	t.buildItems()
	t.render()
	t.loop()
}

func (t *tui) buildItems() {
	t.items = t.items[:0]
	catHasVisible := make(map[string]bool)

	for _, cat := range t.catalog.Categories {
		for _, s := range cat.Skills {
			if t.installed[s.Name] {
				continue
			}
			if t.filter == "" || strings.Contains(strings.ToLower(s.Name), strings.ToLower(t.filter)) {
				catHasVisible[cat.Name] = true
			}
		}
	}

	for _, cat := range t.catalog.Categories {
		if !catHasVisible[cat.Name] {
			continue
		}
		t.items = append(t.items, visibleItem{isHeader: true, name: cat.Name})
		for _, s := range cat.Skills {
			if t.installed[s.Name] {
				continue
			}
			if t.filter == "" || strings.Contains(strings.ToLower(s.Name), strings.ToLower(t.filter)) {
				t.items = append(t.items, visibleItem{
					name:   s.Name,
					source: s.Source,
					desc:   s.Description,
				})
			}
		}
	}

	// Clamp cursor to valid skill range
	skillCount := t.numSkills()
	if skillCount == 0 {
		t.cursor = 0
	} else if t.cursor >= skillCount {
		t.cursor = skillCount - 1
	}
}

func (t *tui) numSkills() int {
	n := 0
	for _, it := range t.items {
		if !it.isHeader {
			n++
		}
	}
	return n
}

func (t *tui) skillIndex(n int) int {
	// Return item index of the nth skill (0-based)
	idx := 0
	for i, it := range t.items {
		if !it.isHeader {
			if idx == n {
				return i
			}
			idx++
		}
	}
	return -1
}

func (t *tui) render() {
	var buf strings.Builder
	buf.WriteString("\x1b[H\x1b[2J") // clear + home

	// Header
	buf.WriteString("\x1b[1mskills-pick\x1b[0m  \x1b[2mselect skills to install\x1b[0m\r\n")
	buf.WriteString("  \x1b[2mj/k nav | space toggle | enter install | / filter | q quit\x1b[0m\r\n")
	buf.WriteString("\r\n")

	skillLines := t.termH - 5 // header(4) + status(1)
	if skillLines < 1 {
		skillLines = 1
	}

	// Build ALL display lines, then slice to viewport
	type line struct{ text string }
	var lines []line
	skillLineIdx := make(map[int]int) // skill cursor index -> display line index
	skillCount := 0

	for _, it := range t.items {
		if it.isHeader {
			lines = append(lines, line{"\x1b[1m\x1b[33m\xe2\x94\x80\xe2\x94\x80 " + it.name + " \xe2\x94\x80\xe2\x94\x80\x1b[0m"})
			continue
		}

		prefix := "[ ]"
		if t.selected[it.name] {
			prefix = "\x1b[32m[\xe2\x9c\x93]\x1b[0m"
		}
		lineText := prefix + " " + it.name
		if t.cursor == skillCount {
			lineText = "\x1b[7m" + lineText + "\x1b[0m"
		} else if t.selected[it.name] {
			lineText = "\x1b[32m" + lineText + "\x1b[0m"
		}
		skillLineIdx[skillCount] = len(lines)
		lines = append(lines, line{lineText})
		skillCount++
	}

	if skillCount > 0 {
		cursorLine := skillLineIdx[t.cursor]
		start := 0
		if len(lines) > skillLines {
			start = cursorLine - skillLines/2
			if start < 0 {
				start = 0
			}
			if end := start + skillLines; end > len(lines) {
				start = len(lines) - skillLines
			}
		}
		for _, l := range lines[start:] {
			if skillLines <= 0 {
				break
			}
			buf.WriteString(l.text + "\r\n")
			skillLines--
		}
	} else {
		for i := 0; i < skillLines; i++ {
			if i == skillLines/2 {
				buf.WriteString("  \x1b[2mno skills match filter\x1b[0m\r\n")
			} else {
				buf.WriteString("\r\n")
			}
		}
	}
	for skillLines > 0 {
		buf.WriteString("\r\n")
		skillLines--
	}

	if t.state == stateFilter {
		buf.WriteString("\x1b[7m Filter: " + t.filter + "\x1b[0m")
		filterLen := len(t.filter) + 10
		if filterLen > t.termW-1 {
			filterLen = t.termW - 1
		}
		buf.WriteString(fmt.Sprintf("\x1b[%dG", filterLen))
	} else {
		selectedCount := 0
		for _, v := range t.selected {
			if v {
				selectedCount++
			}
		}
		if selectedCount > 0 {
			buf.WriteString(fmt.Sprintf("  \x1b[2m%d selected, enter to install\x1b[0m", selectedCount))
		} else {
			buf.WriteString("  \x1b[2mspace to select, enter to install\x1b[0m")
		}
	}

	os.Stdout.WriteString(buf.String())
}

func (t *tui) loop() {
	buf := make([]byte, 16)
	for {
		n, _ := os.Stdin.Read(buf)
		if n == 0 {
			continue
		}
		b := buf[:n]

		// Global: Ctrl+C quits from any state
		if len(b) == 1 && b[0] == 3 {
			return
		}

		if t.state == stateFilter {
			if t.handleFilterInput(b) {
				continue
			}
			return
		}

		if t.handleNormalInput(b) {
			continue
		}
		return
	}
}

func (t *tui) handleNormalInput(b []byte) bool {
	switch {
	case len(b) == 1 && b[0] == 'q':
		return false

	case len(b) == 1 && b[0] == '/':
		t.state = stateFilter
		t.filter = ""
		t.render()
		return true

	case len(b) == 1 && b[0] == 'j':
		fallthrough
	case len(b) == 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'B':
		skillCount := t.numSkills()
		if skillCount > 0 {
			t.cursor = (t.cursor + 1) % skillCount
			t.render()
		}
		return true

	case len(b) == 1 && b[0] == 'k':
		fallthrough
	case len(b) == 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'A':
		skillCount := t.numSkills()
		if skillCount > 0 {
			t.cursor = (t.cursor - 1 + skillCount) % skillCount
			t.render()
		}
		return true

	case len(b) == 1 && b[0] == ' ':
		idx := t.skillIndex(t.cursor)
		if idx >= 0 {
			name := t.items[idx].name
			t.selected[name] = !t.selected[name]
			t.render()
		}
		return true

	case len(b) == 1 && b[0] == 13: // enter
		var selected []Skill
		lookup := buildLookup(t.catalog)
		for name, sel := range t.selected {
			if sel {
				if s, ok := lookup[name]; ok {
					selected = append(selected, s)
				}
			}
		}
		if len(selected) == 0 {
			return false
		}
		term.Restore(int(os.Stdin.Fd()), t.oldState)
		fmt.Println()
		runInstall(selected)
		return false

	case len(b) == 1 && b[0] == 3: // Ctrl+C
		return false

	case len(b) == 1 && b[0] == 0x1b: // Escape (bare)
		return false

	case len(b) == 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'H': // Home
		if t.numSkills() > 0 {
			t.cursor = 0
			t.render()
		}
		return true

	case len(b) == 3 && b[0] == 0x1b && b[1] == '[' && b[2] == 'F': // End
		if n := t.numSkills(); n > 0 {
			t.cursor = n - 1
			t.render()
		}
		return true

	default:
		return true
	}
}

func (t *tui) handleFilterInput(b []byte) bool {
	switch {
	case len(b) == 1 && b[0] == 0x1b: // Escape
		t.filter = ""
		t.state = stateNormal
		t.buildItems()
		t.render()
		return true

	case len(b) == 1 && b[0] == 13: // Enter
		t.state = stateNormal
		t.buildItems()
		t.render()
		return true

	case len(b) == 1 && b[0] == 127: // Backspace
		if len(t.filter) > 0 {
			t.filter = t.filter[:len(t.filter)-1]
			t.buildItems()
			t.render()
		}
		return true

	case len(b) == 3 && b[0] == 0x1b && b[1] == '[' && b[2] == '3': // Delete (escape sequence)
		// Need to read the ~ byte too
		// For simplicity, backspace handles deletion
		return true

	case b[0] >= 32 && b[0] <= 126: // Printable ASCII
		t.filter += string(b[0])
		t.buildItems()
		t.render()
		return true

	default:
		return true
	}
}

func (t *tui) restore() {
	if t.oldState != nil {
		term.Restore(int(os.Stdin.Fd()), t.oldState)
	}
}
