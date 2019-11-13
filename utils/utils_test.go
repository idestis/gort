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
	test := []string{"pineapple", "banana"}

	for i, v := range test {
		_, err := Find(testData, v)
		if !err {
			t.Errorf("%d: Find for %v returned %v want %v", i, v, err, true)
		}
	}

}
