//go:build linux

// active-measure reads only explicitly bound cgroups and local kernel counters.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const maxSeconds = 1800
const maxGroups = 16

type configuration struct {
	Seconds     int         `json:"seconds"`
	Groups      []groupSpec `json:"groups"`
	Observation string      `json:"observation,omitempty"`
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
	path := flag.String("config", "", "private bounded active-measure configuration")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if flag.NArg() != 0 || run(ctx, *path, os.Stdout) != nil {
		fmt.Fprintln(os.Stderr, "active measurement failed; incomplete evidence must not qualify")
		os.Exit(1)
	}
}

func loadConfiguration(input io.Reader) (configuration, error) {
	var cfg configuration
	raw, err := io.ReadAll(io.LimitReader(input, 16385))
	if err != nil || len(raw) > 16384 {
		return cfg, errors.New("configuration exceeds byte bound")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&cfg) != nil || d.Decode(new(any)) != io.EOF || cfg.Seconds < 1 || cfg.Seconds > maxSeconds || len(cfg.Groups) > maxGroups {
		return cfg, errors.New("invalid configuration")
	}
	if cfg.Observation != "" && cfg.Observation != "containment" {
		return cfg, errors.New("unknown observation format")
	}
	roles := map[string]bool{}
	paths := map[string]bool{}
	inodes := map[uint64]bool{}
	for _, spec := range cfg.Groups {
		if !validGroup(spec) || roles[spec.Role] || paths[spec.Path] || inodes[spec.Inode] {
			return cfg, errors.New("invalid group binding")
		}
		roles[spec.Role] = true
		paths[spec.Path] = true
		inodes[spec.Inode] = true
	}
	return cfg, nil
}

func run(ctx context.Context, path string, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("private regular configuration required")
	}
	cfg, err := loadConfiguration(f)
	if err != nil {
		return err
	}
	groups := map[string]*group{}
	defer func() {
		for _, g := range groups {
			g.root.Close()
		}
	}()
	for _, spec := range cfg.Groups {
		g, err := openGroup(spec)
		if err != nil {
			return err
		}
		groups[spec.Role] = g
	}
	origin := time.Now()
	writer := recordWriter{output: output}
	for i := 0; i <= cfg.Seconds; i++ {
		timer := time.NewTimer(time.Until(origin.Add(time.Duration(i) * time.Second)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		begin := time.Now()
		if i == 0 {
			begin = origin
		}
		r := record{SchemaVersion: 2, Index: i, ElapsedNanos: begin.Sub(origin).Nanoseconds(), WallNanos: begin.UnixNano(), Groups: map[string]groupSample{}}
		if cfg.Observation == "containment" {
			r.SchemaVersion = 3
		}
		for role, g := range groups {
			s, err := g.sample()
			if err != nil {
				return err
			}
			if cfg.Observation == "containment" {
				s.Containment, err = g.containment()
				if err != nil {
					return err
				}
			}
			r.Groups[role] = s
		}
		r.Node, err = readNode()
		if err != nil {
			return err
		}
		r.ReadNanos = time.Since(begin).Nanoseconds()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writer.write(r); err != nil {
			return err
		}
	}
	return nil
}
