package main

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

// Retain module notices plus any notices between a compiled package and its
// module root. Vendored forks can have different terms from the enclosing module.
// Do not walk unrelated package trees or follow notice symlinks.
func moduleNoticePaths(directory string, packages []string) ([]string, error) {
	root, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	names, err := notices(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{root: true}
	for _, packageDir := range packages {
		current, err := filepath.EvalSymlinks(packageDir)
		if err != nil {
			return nil, err
		}
		current, err = filepath.Abs(current)
		if err != nil {
			return nil, err
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || !filepath.IsLocal(relative) {
			return nil, errors.New("compiled package is outside its module")
		}
		for !seen[current] {
			seen[current] = true
			files, _, err := noticeFiles(current)
			if err != nil {
				return nil, err
			}
			for _, name := range files {
				names = append(names, filepath.ToSlash(filepath.Join(relative, name)))
			}
			current = filepath.Dir(current)
			relative = filepath.Dir(relative)
		}
	}
	sort.Strings(names)
	return names, nil
}

func noticeFilename(name string) bool {
	// API models such as license.go are source code, not licence notices.
	if strings.EqualFold(filepath.Ext(name), ".go") {
		return false
	}
	upper := strings.ToUpper(name)
	for _, prefix := range []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}
