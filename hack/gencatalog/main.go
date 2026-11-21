// Copyright 2026 The KubeStack Authors. All rights reserved.

// Command gencatalog writes the bundled RKE2 release catalog (internal/upgrade/releases.json) from the
// release metadata (KDM data.json) shipped inside the pinned community Rancher image: the latest patch of
// every minor version that Rancher supports. Build time only; nothing is fetched at runtime (rules 13, 14).
// Usage: go run ./hack/gencatalog -rancher v2.15.3 -kdm data.json > internal/upgrade/releases.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
)

type kdm struct {
	RKE2 struct {
		Releases []struct {
			Version                 string `json:"version"`
			MinChannelServerVersion string `json:"minChannelServerVersion"`
			MaxChannelServerVersion string `json:"maxChannelServerVersion"`
		} `json:"releases"`
	} `json:"rke2"`
}

// nums parses "v1.36.5+rke2r1" or "v2.15.0-alpha1" into [major minor patch]; suffixes are ignored.
func nums(s string) ([3]int, error) {
	core, _, _ := strings.Cut(strings.TrimPrefix(s, "v"), "+")
	core, _, _ = strings.Cut(core, "-")
	p := strings.Split(core, ".")
	var n [3]int
	if len(p) != 3 {
		return n, fmt.Errorf("invalid version %q", s)
	}
	for i, x := range p {
		v, err := strconv.Atoi(x)
		if err != nil {
			return n, fmt.Errorf("invalid version %q", s)
		}
		n[i] = v
	}
	return n, nil
}

func cmp(a, b [3]int) int { return slices.Compare(a[:], b[:]) }

// catalog returns the newest RKE2 release per minor that the given Rancher version supports, oldest minor first.
func catalog(r io.Reader, rancher string) ([]string, error) {
	rv, err := nums(rancher)
	if err != nil {
		return nil, err
	}
	var d kdm
	if err := json.NewDecoder(r).Decode(&d); err != nil {
		return nil, fmt.Errorf("reading KDM data: %w", err)
	}
	latest := map[[2]int][3]int{}
	name := map[[3]int]string{}
	for _, rel := range d.RKE2.Releases {
		v, err := nums(rel.Version)
		if err != nil {
			return nil, err
		}
		lo, err := nums(rel.MinChannelServerVersion)
		if err != nil {
			return nil, err
		}
		hi, err := nums(rel.MaxChannelServerVersion)
		if err != nil {
			return nil, err
		}
		if cmp(lo, rv) > 0 || cmp(rv, hi) > 0 {
			continue
		}
		minor := [2]int{v[0], v[1]}
		if cur, ok := latest[minor]; !ok || cmp(v, cur) > 0 {
			latest[minor] = v
			name[v] = rel.Version
		}
	}
	var out []string
	for _, v := range latest {
		out = append(out, name[v])
	}
	slices.SortFunc(out, func(a, b string) int { x, _ := nums(a); y, _ := nums(b); return cmp(x, y) })
	if len(out) == 0 {
		return nil, fmt.Errorf("KDM data has no RKE2 release for Rancher %s", rancher)
	}
	return out, nil
}

func main() {
	rancher := flag.String("rancher", "", "Rancher version the catalog is for, e.g. v2.15.3")
	path := flag.String("kdm", "", "path to the KDM data.json from that Rancher image")
	flag.Parse()
	f, err := os.Open(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	v, err := catalog(f, *rancher)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(out))
}
