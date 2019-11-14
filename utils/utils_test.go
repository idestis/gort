package utils

import "testing"

func TestScanScripts(t *testing.T) {
	result := ScanScripts("../dist")
	if len(result) > 0 {
		t.Errorf("ScanScripts returned unexpected lenght: got %d want %d", len(result), 0)
	}
}

func TestFind(t *testing.T) {
	testData := []string{"apple", "carrot", "manzana", "pineapple", "melon", "banana"}
	test := map[string]bool{"pineapple": true, "banana": true, "dragonfruit": false}

	for value, expect := range test {
		_, err := Find(testData, value)
		if err != expect {
			t.Errorf("Find for %v returned %v want %v", value, expect, err)
		}
	}

}
