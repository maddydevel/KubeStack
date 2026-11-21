// Copyright 2026 The KubeStack Authors. All rights reserved.

package main

import (
	"slices"
	"strings"
	"testing"
)

const fixture = `{"rke2":{"releases":[
 {"version":"v1.34.11+rke2r1","minChannelServerVersion":"v2.14.0","maxChannelServerVersion":"v2.15.99"},
 {"version":"v1.34.12+rke2r1","minChannelServerVersion":"v2.14.0","maxChannelServerVersion":"v2.15.99"},
 {"version":"v1.35.10+rke2r2","minChannelServerVersion":"v2.15.0-alpha1","maxChannelServerVersion":"v2.15.99"},
 {"version":"v1.35.9+rke2r1","minChannelServerVersion":"v2.15.0-alpha1","maxChannelServerVersion":"v2.15.99"},
 {"version":"v1.33.9+rke2r1","minChannelServerVersion":"v2.13.0","maxChannelServerVersion":"v2.14.99"},
 {"version":"v1.36.9+rke2r1","minChannelServerVersion":"v2.16.0","maxChannelServerVersion":"v2.16.99"}
]}}`

func TestCatalog(t *testing.T) {
	got, err := catalog(strings.NewReader(fixture), "v2.15.3")
	if err != nil {
		t.Fatal(err)
	}
	// Latest patch per minor (numeric, so 1.35.10 beats 1.35.9); out-of-range Rancher versions dropped.
	if want := []string{"v1.34.12+rke2r1", "v1.35.10+rke2r2"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCatalogRefusesEmptyResult(t *testing.T) {
	if _, err := catalog(strings.NewReader(fixture), "v2.99.0"); err == nil {
		t.Fatal("expected an error when no release supports the Rancher version")
	}
	if _, err := catalog(strings.NewReader(`{"rke2":{"releases":[{"version":"garbage"}]}}`), "v2.15.3"); err == nil {
		t.Fatal("expected an error for an unparsable version")
	}
}
