package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Inspect is the RouterOS console tree captured by restraml
// (https://tikoci.github.io/restraml/<version>/inspect.json, optionally gzipped, the same files
// tools/schema_changes ships). It is the oracle for "is this field an add/set argument?".
type Inspect struct {
	root map[string]any
}

// LoadInspect reads an inspect.json or inspect.json.gz file.
func LoadInspect(file string) (*Inspect, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(file, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		r = gz
	}
	return ParseInspect(r)
}

// ParseInspect decodes an inspect tree.
func ParseInspect(r io.Reader) (*Inspect, error) {
	var root map[string]any
	if err := json.NewDecoder(r).Decode(&root); err != nil {
		return nil, fmt.Errorf("inspect json: %w", err)
	}
	return &Inspect{root: root}, nil
}

// inspectMetaArgs are `add`/`set` arguments that are not item properties.
var inspectMetaArgs = map[string]struct{}{"_type": {}, "numbers": {}, "copy-from": {}}

// inspectPositionalArgs are `add` arguments that position an item and are never returned by a
// GET; the provider exposes them selectively (place_before), so they are not reported as missing.
var inspectPositionalArgs = map[string]struct{}{"place-before": {}, "place-after": {}}

// Menu returns the union of the `add` and `set` argument names of a menu ("/ip/service").
// ok is false when the menu is not in the tree (unknown package or hardware on the capture device).
func (i *Inspect) Menu(path string) (args map[string]struct{}, ok bool) {
	if i == nil {
		return nil, false
	}
	node := i.root
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		child, isMap := node[seg].(map[string]any)
		if !isMap {
			return nil, false
		}
		node = child
	}
	if t, _ := node["_type"].(string); t != "dir" {
		return nil, false
	}
	args = make(map[string]struct{})
	found := false
	for _, cmd := range []string{"add", "set"} {
		c, isMap := node[cmd].(map[string]any)
		if !isMap {
			continue
		}
		found = true
		for name, v := range c {
			if _, meta := inspectMetaArgs[name]; meta {
				continue
			}
			if m, isMap := v.(map[string]any); isMap {
				if t, _ := m["_type"].(string); t == "arg" {
					args[name] = struct{}{}
				}
			}
		}
	}
	return args, found
}
