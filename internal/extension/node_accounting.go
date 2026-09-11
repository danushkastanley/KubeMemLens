package extension

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"

	"github.com/danushkastanley/kube-memlens/internal/nodeanalysis"
)

const maxAccountingFileBytes = 1 << 20
const maxAccountingEntries = 1000

var errNodeAccounting = errors.New("invalid bounded Node accounting qualification file")

// LoadNodeAccounting reads operator configuration once at startup. Producer and
// read payloads cannot supply qualifications. Empty configuration is unqualified.
func LoadNodeAccounting(path string) (map[string]nodeanalysis.Qualification, error) {
	entries := map[string]nodeanalysis.Qualification{}
	if path == "" {
		return entries, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errNodeAccounting
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxAccountingFileBytes {
		return nil, errNodeAccounting
	}
	data, err := io.ReadAll(io.LimitReader(file, maxAccountingFileBytes+1))
	if err != nil || len(data) > maxAccountingFileBytes {
		return nil, errNodeAccounting
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, errNodeAccounting
	}
	for decoder.More() {
		if len(entries) >= maxAccountingEntries {
			return nil, errNodeAccounting
		}
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil || len(raw) > 2048 {
			return nil, errNodeAccounting
		}
		item := json.NewDecoder(bytes.NewReader(raw))
		item.DisallowUnknownFields()
		var value nodeanalysis.Qualification
		if item.Decode(&value) != nil || value.Validate() != nil {
			return nil, errNodeAccounting
		}
		if _, exists := entries[value.NodeUID]; exists {
			return nil, errNodeAccounting
		}
		entries[value.NodeUID] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim(']') {
		return nil, errNodeAccounting
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errNodeAccounting
	}
	return entries, nil
}

func copyAccounting(values map[string]nodeanalysis.Qualification) map[string]nodeanalysis.Qualification {
	result := make(map[string]nodeanalysis.Qualification, len(values))
	for uid, value := range values {
		value.DisjointSystems = slices.Clone(value.DisjointSystems)
		result[uid] = value
	}
	return result
}
