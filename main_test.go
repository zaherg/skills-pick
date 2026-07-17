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
