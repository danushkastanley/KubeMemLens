package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/danushkastanley/kube-memlens/prototype/trace/qualification/verifier"
)

var errConfiguration = errors.New("invalid fixed verifier calibration configuration")
var bootPattern = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

func readConfiguration(input io.Reader) (config, error) {
	var c config
	raw, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(raw) == 0 || len(raw) > 16384 {
		return c, errConfiguration
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return c, errConfiguration
	}
	keys := map[string]bool{"Owner": false, "BTF": false, "Boot": false, "SelectedGroup": false, "OtherGroup": false,
		"SelectedInode": false, "OtherInode": false, "CPUs": false}
	for decoder.More() {
		token, err := decoder.Token()
		name, ok := token.(string)
		seen, known := keys[name]
		if err != nil || !ok || !known || seen {
			return c, errConfiguration
		}
		keys[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return c, errConfiguration
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return c, errConfiguration
	}
	if _, err := decoder.Token(); err != io.EOF {
		return c, errConfiguration
	}
	for _, seen := range keys {
		if !seen {
			return c, errConfiguration
		}
	}
	if json.Unmarshal(raw, &c) != nil {
		return c, errConfiguration
	}
	return c, validateConfiguration(c)
}

func validateConfiguration(c config) error {
	if _, err := verifier.NewProbePlan(c.Owner); err != nil {
		return errConfiguration
	}
	btf, err := hex.DecodeString(c.BTF)
	if err != nil || len(btf) != 32 || hex.EncodeToString(btf) != c.BTF || !bootPattern.MatchString(c.Boot) {
		return errConfiguration
	}
	prefix := "/sys/fs/cgroup/kml-vcal-" + c.Owner
	if c.SelectedGroup != prefix+"-selected" || c.OtherGroup != prefix+"-excluded" ||
		c.SelectedInode == 0 || c.OtherInode == 0 || c.SelectedInode == c.OtherInode || len(c.CPUs) < 2 || len(c.CPUs) > 64 {
		return errConfiguration
	}
	for index, cpu := range c.CPUs {
		if cpu >= 1024 || (index > 0 && cpu <= c.CPUs[index-1]) {
			return errConfiguration
		}
	}
	return nil
}
