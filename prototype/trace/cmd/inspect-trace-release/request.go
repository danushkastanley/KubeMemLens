package main

import (
	json "encoding/json/v2"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/danushkastanley/kube-memlens/prototype/trace/releasebundle"
)

// This input is derived from the externally authenticated release manifest. It
// is not a second release inventory or an authentication decision by this tool.
type request struct {
	SchemaVersion  int       `json:"schemaVersion"`
	ReleaseVersion string    `json:"releaseVersion"`
	SourceCommit   string    `json:"sourceCommit"`
	ImageDigest    string    `json:"imageDigest"`
	Payloads       []payload `json:"payloads"`
}

type payload struct {
	Role   string `json:"role"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

var filename = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var hash = regexp.MustCompile(`^[a-f0-9]{64}$`)

func readRequest(source io.Reader) (request, error) {
	var value request
	data, err := io.ReadAll(io.LimitReader(source, 16385))
	if err != nil || len(data) > 16384 || json.Unmarshal(data, &value, json.RejectUnknownMembers(true)) != nil || value.SchemaVersion != 1 || len(value.Payloads) != 4 {
		return request{}, releasebundle.ErrArchive
	}
	roles := []string{"chart", "engine", "image", "programmes"}
	limits := []int64{8 << 20, 512 << 20, 1 << 30, 128 << 20}
	names := make(map[string]bool)
	for index, item := range value.Payloads {
		if item.Role != roles[index] || !filename.MatchString(item.Name) || strings.Contains(item.Name, "..") || names[item.Name] || !hash.MatchString(item.SHA256) || item.Size <= 0 || item.Size > limits[index] {
			return request{}, releasebundle.ErrArchive
		}
		names[item.Name] = true
	}
	return value, nil
}

func openPayload(root *os.Root, item payload) (*os.File, error) {
	before, err := root.Lstat(item.Name)
	if err != nil || !before.Mode().IsRegular() || before.Size() != item.Size {
		return nil, releasebundle.ErrArchive
	}
	file, err := root.Open(item.Name)
	if err != nil {
		return nil, releasebundle.ErrArchive
	}
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) || !after.Mode().IsRegular() || after.Size() != item.Size {
		file.Close()
		return nil, releasebundle.ErrArchive
	}
	return file, nil
}
