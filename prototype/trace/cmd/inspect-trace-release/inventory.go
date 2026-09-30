package main

import "github.com/danushkastanley/kube-memlens/prototype/trace/releasebundle"

type fileDigest struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

type fileInventory struct {
	Role     string       `json:"role"`
	Platform string       `json:"platform"`
	Files    []fileDigest `json:"files"`
}

func inventory(role, platform string, files releasebundle.FileSet) fileInventory {
	result := fileInventory{Role: role, Platform: platform, Files: []fileDigest{}}
	for _, name := range files.Names() {
		entry, _ := files.Entry(name)
		if entry.Type == releasebundle.Regular {
			result.Files = append(result.Files, fileDigest{Name: name, SHA256: entry.SHA256})
		}
	}
	return result
}

func inventories(chart releasebundle.Chart, engine releasebundle.Engine, image releasebundle.Image, programmes releasebundle.Programmes) []fileInventory {
	result := []fileInventory{inventory("chart", "independent", chart.Files()), inventory("engine", "independent", engine.Files())}
	for _, architecture := range []string{"amd64", "arm64"} {
		files, _ := image.Platform(architecture)
		result = append(result, inventory("image", "linux/"+architecture, files))
	}
	return append(result, inventory("programmes", "independent", programmes.Files()))
}
