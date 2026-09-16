//go:build linux

// Administrative, read-only measurement of explicitly supplied cgroups.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

type configuration struct {
	Seconds int         `json:"seconds"`
	Groups  []groupSpec `json:"groups"`
}

type record struct {
	SchemaVersion int                    `json:"schemaVersion"`
	Index         int                    `json:"index"`
	ElapsedNanos  int64                  `json:"elapsedNanos"`
	WallNanos     int64                  `json:"wallNanos"`
	ReadNanos     int64                  `json:"readNanos"`
	Groups        map[string]groupSample `json:"groups"`
	Node          nodeSample             `json:"node"`
}

func main() {
	path := flag.String("config", "", "private, bounded measurement configuration")
	flag.Parse()
	if flag.NArg() != 0 || run(*path, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "measurement failed; incomplete evidence must not qualify")
		os.Exit(1)
	}
}

func run(path string, output io.Writer) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var cfg configuration
	d := json.NewDecoder(io.LimitReader(f, 8193))
	d.DisallowUnknownFields()
	if err := d.Decode(&cfg); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF || cfg.Seconds < 1 || cfg.Seconds > 900 || len(cfg.Groups) > 2 {
		return errors.New("invalid configuration")
	}
	groups := make(map[string]*group)
	defer func() {
		for _, g := range groups {
			g.root.Close()
		}
	}()
	for _, spec := range cfg.Groups {
		if groups[spec.Role] != nil {
			return errors.New("duplicate role")
		}
		g, err := openGroup(spec)
		if err != nil {
			return err
		}
		groups[spec.Role] = g
	}
	origin := time.Now()
	encoder := json.NewEncoder(output)
	for i := 0; i <= cfg.Seconds; i++ {
		if i > 0 {
			time.Sleep(time.Until(origin.Add(time.Duration(i) * time.Second)))
		}
		begin := time.Now()
		if i == 0 {
			begin = origin
		}
		r := record{SchemaVersion: 1, Index: i, ElapsedNanos: begin.Sub(origin).Nanoseconds(), WallNanos: begin.UnixNano(), Groups: map[string]groupSample{}}
		for role, g := range groups {
			sample, err := g.sample()
			if err != nil {
				return err
			}
			r.Groups[role] = sample
		}
		r.Node, err = readNode()
		if err != nil {
			return err
		}
		r.ReadNanos = time.Since(begin).Nanoseconds()
		if err := encoder.Encode(r); err != nil {
			return err
		}
	}
	return nil
}
