// schema-drift compares the provider's resource schemas with what a live RouterOS device
// returns for each menu and reports the fields that exist on one side only.
//
// Run from the repository root:
//
//	ROS_HOSTURL=https://router ROS_USERNAME=admin ROS_PASSWORD=... go run ./tools/schema-drift -md report.md -json report.json
//
// See tools/schema-drift/README.md for the full recipe.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

func main() {
	var (
		schemaFile   = flag.String("schema", "", "provider schema JSON from `terraform providers schema -json` (default: the schemas compiled into this tool)")
		inspectFile  = flag.String("inspect", "", "restraml inspect.json[.gz] for the device's RouterOS version; marks fields writable/read-only")
		resources    = flag.String("resources", "", "comma-separated resource names and/or menu paths to compare (default: all)")
		statusFields = flag.String("status-fields", "", "comma-separated extra device fields to treat as read-only")
		mdOut        = flag.String("md", "-", "write the Markdown report to this file ('-' = stdout, '' = none)")
		jsonOut      = flag.String("json", "", "write the JSON report to this file ('-' = stdout)")
		all          = flag.Bool("all", false, "include covered fields in the Markdown tables")
		failOnDrift  = flag.Bool("fail-on-missing", false, "exit 2 when any field is classified as missing")
		timeout      = flag.Duration("timeout", 60*time.Second, "HTTP timeout per request")
		concurrency  = flag.Int("concurrency", 4, "parallel GET requests")
	)
	flag.Parse()
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	code, err := run(*schemaFile, *inspectFile, *resources, *statusFields, *mdOut, *jsonOut, *all, *failOnDrift, *timeout, *concurrency)
	if err != nil {
		log.Fatalf("schema-drift: %v", err)
	}
	os.Exit(code)
}

func run(schemaFile, inspectFile, resources, statusFields, mdOut, jsonOut string, all, failOnDrift bool,
	timeout time.Duration, concurrency int) (int, error) {

	report := &Report{GeneratedAt: time.Now(), IncludeCovered: all, SchemaSource: "compiled provider" + buildRevision()}

	// 1. Provider side: menu mapping always comes from the compiled provider; attributes
	//    optionally from a schema JSON so that a released binary can be checked too.
	mapping := ProviderResources(routeros.Provider())
	if schemaFile != "" {
		attrs, err := LoadSchemaJSON(schemaFile)
		if err != nil {
			return 1, err
		}
		report.SchemaSource = "`" + schemaFile + "`"
		filtered := make(map[string]*Resource, len(attrs))
		for name, a := range attrs {
			r, ok := mapping[name]
			if !ok {
				report.Unmapped = append(report.Unmapped, name)
				continue
			}
			cp := *r
			cp.Attrs = a
			filtered[name] = &cp
		}
		sort.Strings(report.Unmapped)
		mapping = filtered
	}
	groups, noMenu := GroupByMenu(mapping)
	for _, r := range noMenu {
		report.NoMenu = append(report.NoMenu, r.Name)
	}

	var filter []string
	if resources != "" {
		filter = strings.Split(resources, ",")
	}
	var selected []*MenuGroup
	for _, g := range groups {
		if g.MatchesFilter(filter) {
			selected = append(selected, g)
		}
	}
	if len(selected) == 0 {
		return 1, fmt.Errorf("no resource matched -resources %q", resources)
	}

	// 2. Oracle for writability (optional).
	var inspect *Inspect
	if inspectFile != "" {
		var err error
		if inspect, err = LoadInspect(inspectFile); err != nil {
			return 1, err
		}
		report.InspectSource = "`" + inspectFile + "`"
	}

	// 3. Device side.
	client, err := NewClientFromEnv(timeout)
	if err != nil {
		return 1, err
	}
	report.Device = client.Base
	version, err := client.Version()
	if err != nil {
		return 1, fmt.Errorf("reading /system/resource: %w", err)
	}
	report.RouterOS = version
	log.Printf("device %s runs RouterOS %s; comparing %d menus", client.Base, version, len(selected))

	var extraStatus []string
	if statusFields != "" {
		extraStatus = strings.Split(statusFields, ",")
	}
	comparer := NewComparer(version, inspect, extraStatus)

	results := make([]*Comparison, len(selected))
	var wg sync.WaitGroup
	sem := make(chan struct{}, max(concurrency, 1))
	var firstErr error
	var mu sync.Mutex
	for i, g := range selected {
		wg.Add(1)
		go func(i int, g *MenuGroup) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			items, err := client.Get(g.Path)
			switch {
			case errors.Is(err, ErrMenuAbsent):
				results[i] = SkippedComparison(g, err.Error())
				return
			case err != nil:
				results[i] = SkippedComparison(g, "GET failed: "+err.Error())
				return
			}
			cmp, err := comparer.Compare(g, items)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("%s: %w", g.Path, err)
				}
				mu.Unlock()
				return
			}
			results[i] = cmp
		}(i, g)
	}
	wg.Wait()
	if firstErr != nil {
		return 1, firstErr
	}
	report.Menus = results
	report.Summarize()

	// 4. Output.
	if mdOut != "" {
		if err := writeTo(mdOut, report.WriteMarkdown); err != nil {
			return 1, err
		}
	}
	if jsonOut != "" {
		if err := writeTo(jsonOut, report.WriteJSON); err != nil {
			return 1, err
		}
	}
	s := report.Summary
	log.Printf("menus %d (compared %d, skipped %d): missing %d, read-only %d, schema-only %d, covered %d",
		s.Menus, s.Compared, s.Skipped, s.Missing, s.ReadOnly, s.SchemaOnly, s.Covered)
	if failOnDrift && s.Missing > 0 {
		return 2, nil
	}
	return 0, nil
}

// buildRevision returns " (git <rev>[+dirty])" when the Go toolchain stamped VCS information.
func buildRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var rev, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "+dirty"
			}
		}
	}
	if rev == "" {
		return ""
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return " (git " + rev + modified + ")"
}

func writeTo(dest string, fn func(io.Writer) error) error {
	if dest == "-" {
		return fn(os.Stdout)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}
