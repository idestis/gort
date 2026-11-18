package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanScripts(t *testing.T) {
	dir := t.TempDir()
	if result := ScanScripts(dir); len(result) > 0 {
		t.Errorf("ScanScripts returned unexpected length: got %d want %d", len(result), 0)
	}

	if result := ScanScripts(filepath.Join(dir, "missing")); len(result) > 0 {
		t.Errorf("ScanScripts on missing dir returned %d entries, want 0", len(result))
	}

	for _, name := range []string{"b.sh", "a.sh"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("echo hi\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	result := ScanScripts(dir)
	if len(result) != 2 || result[0] != "a.sh" || result[1] != "b.sh" {
		t.Errorf("ScanScripts returned %v, want [a.sh b.sh]", result)
	}
}

func TestFind(t *testing.T) {
	testData := []string{"apple", "carrot", "manzana", "pineapple", "melon", "banana"}
	test := map[string]bool{"pineapple": true, "banana": true, "dragonfruit": false}

	for value, expect := range test {
		_, found := Find(testData, value)
		if found != expect {
			t.Errorf("Find for %v returned %v want %v", value, found, expect)
		}
	}
}
