// Package utils provides helpers to make gort working such as
// directory scanners or slice search
package utils

import "io/ioutil"

// Find takes a slice and looks for an element in it. If found it will
// return it's key, otherwise it will return -1 and a bool of false.
func Find(slice []string, val string) (int, bool) {
	for i, item := range slice {
		if item == val {
			return i, true
		}
	}
	return -1, false
}

// ScanScripts will parse folder to get scripts list on startup
func ScanScripts(dir string) []string {
	var data []string
	scriptsList, _ := ioutil.ReadDir(dir)
	for _, s := range scriptsList {
		data = append(data, s.Name())
	}
	return data
}
