package tspath

import "strings"

var ignoredPaths = []string{"/node_modules/.", "/.git", "/.#", ".pnpm"}

func ContainsIgnoredPath(path string) bool {
	for _, p := range ignoredPaths {
		if strings.Contains(path, p) {
			return true
		}
	}
	return false
}
