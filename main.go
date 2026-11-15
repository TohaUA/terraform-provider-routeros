package main

import (
	"flag"

	"github.com/hashicorp/terraform-plugin-sdk/v2/plugin"
	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
)

// Generate the Terraform provider documentation using `tfplugindocs`:
// Generate catalog metadata before tfplugindocs compiles the provider schema.
//go:generate go run ./tools/schema_changes/generate_7_25.go -input tools/schema_changes/routeros-7.25-api.json -output routeros/routeros_7_25_catalog.go
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs

func main() {
	var debug bool

	// https://github.com/hashicorp/terraform-docs-common/blob/main/website/docs/plugin/debugging.mdx
	// https://developer.hashicorp.com/terraform/plugin/debugging
	flag.BoolVar(&debug, "debug", false, "set to true to run the provider with support for debuggers like delve")
	flag.Parse()

	plugin.Serve(&plugin.ServeOpts{
		ProviderAddr: "terraform-routeros/routeros",
		ProviderFunc: routeros.NewProvider,
		Debug:        debug,
	})
}
