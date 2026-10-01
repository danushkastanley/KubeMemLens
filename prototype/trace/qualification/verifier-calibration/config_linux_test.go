package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func configurationFixture() config {
	owner := strings.Repeat("a", 32)
	return config{Owner: owner, BTF: strings.Repeat("b", 64), Boot: "11111111-1111-1111-1111-111111111111",
		SelectedGroup: "/sys/fs/cgroup/kml-vcal-" + owner + "-selected", OtherGroup: "/sys/fs/cgroup/kml-vcal-" + owner + "-excluded",
		SelectedInode: 10, OtherInode: 20, CPUs: []uint32{0, 1}}
}

func TestConfigurationIsCompleteUnambiguousAndBounded(t *testing.T) {
	c := configurationFixture()
	raw, _ := json.Marshal(c)
	if _, err := readConfiguration(strings.NewReader(string(raw))); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"null", "[]", string(raw) + "{}", strings.Repeat(" ", 16385),
		strings.Replace(string(raw), `"Owner":`, `"Owner":"`+c.Owner+`","Owner":`, 1),
		strings.Replace(string(raw), `"Owner":`, `"owner":`, 1),
		strings.Replace(string(raw), `"CPUs":[0,1]`, `"CPUs":null`, 1),
		strings.Replace(string(raw), `"SelectedInode":10,`, "", 1),
	} {
		if _, err := readConfiguration(strings.NewReader(value)); err == nil {
			t.Fatal("malformed configuration accepted")
		}
	}
}

func TestConfigurationCannotWidenFixtureScope(t *testing.T) {
	for _, change := range []func(*config){
		func(c *config) { c.Owner = "../../foreign" }, func(c *config) { c.BTF = "unbound" },
		func(c *config) { c.Boot = "unbound" }, func(c *config) { c.OtherGroup = c.SelectedGroup },
		func(c *config) { c.OtherInode = c.SelectedInode }, func(c *config) { c.SelectedGroup = "/sys/fs/cgroup" },
		func(c *config) { c.CPUs = []uint32{0} }, func(c *config) { c.CPUs = []uint32{1, 0} },
		func(c *config) { c.CPUs = []uint32{0, 1024} }, func(c *config) { c.SelectedInode = 0 },
	} {
		c := configurationFixture()
		change(&c)
		if validateConfiguration(c) == nil {
			t.Fatal("unowned or unbounded fixture scope accepted")
		}
	}
}
