package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestVersionAndUsageDefaultToDev(t *testing.T) {
	original := version
	version = "dev"
	t.Cleanup(func() { version = original })

	if !strings.Contains(versionOutput(), "skills-pick dev") {
		t.Fatalf("version output does not contain default version: %q", versionOutput())
	}
	if !strings.Contains(usageText(), "skills-pick dev") {
		t.Fatalf("usage does not contain default version: %q", usageText())
	}
}

func TestInjectedVersionRendersConsistently(t *testing.T) {
	original := version
	version = "0.4.0"
	t.Cleanup(func() { version = original })

	if got := versionOutput(); got != "skills-pick 0.4.0\n" {
		t.Fatalf("version output = %q", got)
	}
	if !strings.Contains(usageText(), "skills-pick 0.4.0") {
		t.Fatalf("usage does not contain injected version: %q", usageText())
	}
}

func TestEmbeddedCatalogIsValid(t *testing.T) {
	cat := loadCatalog()
	if cat.Version < 1 || len(cat.Categories) == 0 {
		t.Fatalf("embedded catalog has invalid structure: %#v", cat)
	}
	for _, category := range cat.Categories {
		for _, skill := range category.Skills {
			if skill.Name == "" || skill.Source == "" || skill.Description == "" {
				t.Fatalf("embedded catalog contains incomplete skill: %#v", skill)
			}
		}
	}
}

func TestInvalidCatalogOverrideExitsWithParseError(t *testing.T) {
	if os.Getenv("SKILLS_PICK_INVALID_CATALOG_TEST") == "1" {
		catalogFlag = os.Getenv("SKILLS_PICK_INVALID_CATALOG_PATH")
		loadCatalog()
		return
	}

	path := t.TempDir() + "/invalid-catalog.json"
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run", "^TestInvalidCatalogOverrideExitsWithParseError$")
	cmd.Env = append(os.Environ(), "SKILLS_PICK_INVALID_CATALOG_TEST=1", "SKILLS_PICK_INVALID_CATALOG_PATH="+path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected invalid catalog subprocess to fail; output: %s", output)
	}
	if !strings.Contains(string(output), "failed to parse catalog") {
		t.Fatalf("unexpected invalid catalog output: %s", output)
	}
}

func TestTUIBuildItemsHidesInstalledSkillsRegardlessOfCategory(t *testing.T) {
	coreSkill := Skill{Name: "ce-plan", Source: "source/core", Description: "plan"}
	engineeringSkill := Skill{Name: "go-tool", Source: "source/engineering", Description: "tool"}
	designSkill := Skill{Name: "design-tool", Source: "source/design", Description: "design"}
	state := &tui{
		catalog: &Catalog{Categories: []Category{
			{Name: "Core", Skills: []Skill{coreSkill}},
			{Name: "Engineering", Skills: []Skill{engineeringSkill}},
			{Name: "Design", Skills: []Skill{designSkill}},
		}},
		installed: map[string]bool{
			coreSkill.Name:        true,
			engineeringSkill.Name: true,
		},
	}

	state.buildItems()

	if len(state.items) != 2 {
		t.Fatalf("items = %#v, want only the uninstalled skill and its header", state.items)
	}
	if !state.items[0].isHeader || state.items[0].name != "Design" {
		t.Fatalf("first item = %#v, want Design header", state.items[0])
	}
	if state.items[1].isHeader || state.items[1].name != designSkill.Name {
		t.Fatalf("second item = %#v, want uninstalled Design skill", state.items[1])
	}
	if got := state.numSkills(); got != 1 {
		t.Fatalf("numSkills() = %d, want 1", got)
	}
}

func TestTUIBuildItemsDoesNotRevealInstalledSkillWhenFiltering(t *testing.T) {
	coreSkill := Skill{Name: "ce-plan", Source: "source/core", Description: "plan"}
	state := &tui{
		catalog:   &Catalog{Categories: []Category{{Name: "Core", Skills: []Skill{coreSkill}}}},
		installed: map[string]bool{coreSkill.Name: true},
		filter:    "CE-PLAN",
	}

	state.buildItems()

	if len(state.items) != 0 {
		t.Fatalf("filtered items = %#v, want installed skill to stay hidden", state.items)
	}
}

func TestTUIBuildItemsPreservesOrderAndClampsCursor(t *testing.T) {
	coreSkill := Skill{Name: "ce-plan", Source: "source/core", Description: "plan"}
	engineeringSkill := Skill{Name: "go-tool", Source: "source/engineering", Description: "tool"}
	designSkill := Skill{Name: "design-tool", Source: "source/design", Description: "design"}
	state := &tui{
		catalog: &Catalog{Categories: []Category{
			{Name: "Core", Skills: []Skill{coreSkill}},
			{Name: "Engineering", Skills: []Skill{engineeringSkill}},
			{Name: "Design", Skills: []Skill{designSkill}},
		}},
		installed: map[string]bool{coreSkill.Name: true},
		cursor:    99,
	}

	state.buildItems()

	if got := []string{state.items[0].name, state.items[1].name, state.items[2].name, state.items[3].name}; strings.Join(got, ",") != "Engineering,go-tool,Design,design-tool" {
		t.Fatalf("item order = %v", got)
	}
	if got := state.cursor; got != 1 {
		t.Fatalf("cursor = %d, want last selectable skill index 1", got)
	}

	state.filter = "go-tool"
	state.buildItems()
	if got := state.cursor; got != 0 {
		t.Fatalf("filtered cursor = %d, want 0", got)
	}
}
