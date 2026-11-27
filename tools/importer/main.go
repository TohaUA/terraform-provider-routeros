package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/sirupsen/logrus"
	"github.com/terraform-routeros/terraform-provider-routeros/routeros"
	"golang.org/x/term"
)

var (
	log, _ = NewLogger(context.Background())

	// .id=*1;available=101;name=dhcp;ranges=192.168.88.100-192.168.88.200;total=101;used=0
	reId = regexp.MustCompile(`\.id=(\S+?);`)

	providerTemplate = `terraform {
  required_providers {
    routeros = {
      source  = "terraform-routeros/routeros"
      version = "~> 1"
    }
  }
}

provider "routeros" {
  hosturl  = %s
  username = %s
  # password       = "" # env ROS_PASSWORD or MIKROTIK_PASSWORD
  # ca_certificate = "/path/to/ca/certificate.pem" # env ROS_CA_CERTIFICATE or MIKROTIK_CA_CERTIFICATE
}

`

	importTemplate = `import {
  to = "%v.%v"
  id = %s
}
`
	resourceTempate = `resource "%v" "%v" {
  %v
}

`
)

func main() {
	host := flag.String("host", "192.168.180.171", "Mikrotik host")
	user := flag.String("user", "admin", "Mikrotik user")
	knownHosts := flag.String("known-hosts", defaultKnownHostsPath(), "Path to OpenSSH known_hosts file")
	hostKeyFingerprint := flag.String("host-key-fingerprint", "", "Expected OpenSSH SHA-256 host key fingerprint (SHA256:...)")
	insecureIgnoreHostKey := flag.Bool("insecure-ignore-host-key", false, "Disable SSH host key verification (unsafe; vulnerable to MITM attacks)")
	logLevel := flag.String("loglevel", "info", "Log level (debug, info, warn, error, fatal, panic)")
	flag.Parse()

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Invalid log level specified:", err)
		os.Exit(1)
	}
	log.SetLevel(level)

	hostKeyCallback, err := BuildHostKeyCallback(HostKeyOptions{
		KnownHostsPath:        *knownHosts,
		HostKeyFingerprint:    *hostKeyFingerprint,
		InsecureIgnoreHostKey: *insecureIgnoreHostKey,
	}, os.Stderr)
	if err != nil {
		log.Fatal(err)
	}

	password, err := resolvePassword(os.LookupEnv, readPasswordFromTerminal)
	if err != nil {
		log.Fatal(err)
	}

	conn, err := NewSsh(*host, *user, password, hostKeyCallback)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	config, err := GetMikrotikConfig(conn)
	if err != nil {
		log.Fatal(err)
	}

	if len(config) == 0 {
		log.Fatal("Config is empty")
	}

	provider := routeros.NewProvider()
	hclOutput := generateHCL(config, provider, func(path string, filter []string) string {
		return GetResourceId(conn, path, filter)
	}, *host, *user)

	baseName := fmt.Sprintf("autoimport-%v", time.Now().Format("20060102-1504"))
	if err := writeTFFile(baseName+".tf", []byte(hclOutput)); err != nil {
		log.Fatal(err)
	}

	log.Info("Complete")
}

func resolvePassword(lookupEnv func(string) (string, bool), promptFn func(string) (string, error)) (string, error) {
	for _, key := range []string{"ROS_PASSWORD", "MIKROTIK_PASSWORD"} {
		if val, ok := lookupEnv(key); ok && val != "" {
			return val, nil
		}
	}
	return promptFn("Enter Mikrotik password: ")
}

func readPasswordFromTerminal(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("failed to read password: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func writeTFFile(path string, content []byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func formatHostForURL(host string) string {
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "[" + host + "]"
	}
	return host
}

func renderProviderBlock(host, user string) string {
	hostURL := formatHCLString("https://" + formatHostForURL(host))
	username := formatHCLString(user)
	return fmt.Sprintf(providerTemplate, hostURL, username)
}

func buildProviderResources(provider *schema.Provider) map[string][]string {
	providerResources := make(map[string][]string)
	for k, v := range provider.ResourcesMap {
		path := v.Schema[routeros.MetaResourcePath].Default.(string)
		// Don't add aliases
		if _, ok := providerAliasesMap[k]; ok {
			continue
		}
		providerResources[path] = append(providerResources[path], k)
	}
	for path := range providerResources {
		slices.Sort(providerResources[path])
	}
	return providerResources
}

func generateHCL(config string, provider *schema.Provider, lookupID func(path string, filter []string) string, host, user string) string {
	resHcl := bytes.NewBufferString(renderProviderBlock(host, user))
	providerResources := buildProviderResources(provider)
	hclNames := make(map[string]int)

	for _, cmd := range ParseExport(config) {
		hclSection, err := GetResourceSection(hclNames, providerResources, cmd.Path)
		if err != nil {
			log.Error(err)
			continue
		}

		if cmd.Action == "set" && isResourceWithoutID(hclSection.ResourceName) {
			log.Infof("Skipping resource %v which has no ID", hclSection.ResourceName)
			continue
		}

		hclAttributes, parsedAttrs, required := GetAttributes(provider, hclSection.ResourceName, cmd)

		var id string
		switch {
		case cmd.Action == "add" && usesFullFilterForID(hclSection.ResourceName):
			filter := buildAddFilter(parsedAttrs)
			id = lookupID(cmd.Path, filter)
		case cmd.Action == "set" && usesSetFilterForID(hclSection.ResourceName):
			filter := buildSetFilter(parsedAttrs)
			id = lookupID(cmd.Path, filter)
		default:
			id = lookupID(cmd.Path, required)
		}

		fmt.Fprintf(resHcl, importTemplate, hclSection.ResourceName, hclSection.HCLName, formatHCLString(id))
		fmt.Fprintf(resHcl, resourceTempate, hclSection.ResourceName, hclSection.HCLName, strings.Join(hclAttributes, "\n"))
	}

	return resHcl.String()
}

func isResourceWithoutID(resourceName string) bool {
	switch resourceName {
	case "routeros_capsman_manager",
		"routeros_interface_wifi_capsman",
		"routeros_ip_cloud",
		"routeros_ip_dns",
		"routeros_ip_ssh_server",
		"routeros_ip_upnp",
		"routeros_system_clock",
		"routeros_system_identity",
		"routeros_system_logging",
		"routeros_system_ntp_client",
		"routeros_system_ntp_server",
		"routeros_system_routerboard_settings",
		"routeros_tool_bandwidth_server",
		"routeros_tool_mac_server",
		"routeros_tool_mac_server_winbox",
		"routeros_tool_mac_server_ping",
		"routeros_tool_sniffer",
		"routeros_wifi_capsman":
		return true
	default:
		return false
	}
}

func usesFullFilterForID(resourceName string) bool {
	switch resourceName {
	case "routeros_interface_bridge_filter",
		"routeros_ip_address",
		"routeros_ip_dhcp_server_network",
		"routeros_ip_firewall_filter", "routeros_ip_firewall_mangle", "routeros_ip_firewall_nat", "routeros_ip_firewall_raw",
		"routeros_ipv6_firewall_filter", "routeros_ipv6_firewall_mangle", "routeros_ipv6_firewall_nat",
		"routeros_ip_dns_record",
		"routeros_ip_route",
		"routeros_wifi_provisioning":
		return true
	default:
		return false
	}
}

func usesSetFilterForID(resourceName string) bool {
	switch resourceName {
	case "routeros_interface_ethernet",
		"routeros_interface_lte_apn",
		"routeros_interface_wireless_security_profiles",
		"routeros_ip_ipsec_proposal",
		"routeros_ip_service":
		return true
	default:
		return false
	}
}

func buildAddFilter(parsedAttrs []ParsedAttr) []string {
	var filter []string
	for _, attr := range parsedAttrs {
		if attr.SnakeName == "ntp_server" ||
			attr.SnakeName == "dns_server" ||
			attr.SnakeName == "ttl" ||
			(attr.SnakeName == "gateway" && !attr.IsPlaceholder) ||
			attr.SnakeName == "supported_bands" ||
			attr.SnakeName == "slave_configurations" {
			continue
		}
		val := attr.RawRouterOSValue
		if attr.SchemaType == schema.TypeBool {
			val = routeros.BoolToMikrotikJSONStr(unescapeRouterOSValue(val))
		}
		filter = append(filter, routeros.SnakeToKebab(attr.SnakeName)+"="+val)
	}
	return filter
}

func buildSetFilter(parsedAttrs []ParsedAttr) []string {
	var filter []string
	for _, attr := range parsedAttrs {
		key := attr.SnakeName
		if key == "numbers" {
			key = "name"
		}
		if key == "factory_name" ||
			key == "address" ||
			key == "enc_algorithms" ||
			(key == "port" && attr.IsPlaceholder) {
			continue
		}
		val := attr.RawRouterOSValue
		if key == "name" && (attr.IsPlaceholder || val == ".") {
			val = "default"
		}
		if attr.SchemaType == schema.TypeBool {
			val = routeros.BoolToMikrotikJSONStr(unescapeRouterOSValue(val))
		}
		filter = append(filter, routeros.SnakeToKebab(key)+"="+val)
	}
	return filter
}

// HCLResource represents a Terraform resource type and HCL block name.
type HCLResource struct {
	ResourceName string
	HCLName      string
}

// GetResourceSection returns the resource name in provider notation and a unique HCL identifier.
func GetResourceSection(hclNames map[string]int, providerResources map[string][]string, path string) (*HCLResource, error) {
	// /ip pool => /ip/pool
	path = strings.ReplaceAll(path, " ", "/")
	// /ip/pool => routeros_ip_pool
	resNames, ok := providerResources[path]
	if !ok {
		return nil, fmt.Errorf("no resource was found for '%v' path", path)
	}

	if len(resNames) == 0 {
		return nil, fmt.Errorf("path '%v' exists, but no resource name was found for it", path)
	}

	// Several resources can have the same path (sorted deterministically).
	resourceName := resNames[0]

	// routeros_ip_pool => ip-pool-N
	hclName := strings.ReplaceAll(strings.TrimPrefix(resourceName, "routeros_"), "_", "-")
	hclNames[hclName]++
	hclName = fmt.Sprintf("%v-%v", hclName, hclNames[hclName])

	return &HCLResource{resourceName, hclName}, nil
}

// routeros_interface_ethernet default_name=ether1 ===> factory_name=ether1, name=ether1
var TransformMap = map[string][]string{
	"routeros_interface_ethernet:default_name": {"factory_name", "name"},
}

type ParsedAttr struct {
	SnakeName        string
	HCLValue         string
	RawRouterOSValue string
	SchemaType       schema.ValueType
	IsPlaceholder    bool
}

// GetAttributes returns HCL-formatted attributes, structured attributes for ID filters, and required RouterOS pairs.
func GetAttributes(provider *schema.Provider, resourceName string, cmd ExportCommand) (hclAttributes []string, parsedAttrs []ParsedAttr, required []string) {
	resource := provider.ResourcesMap[resourceName]

	attrRequired := make(map[string]schema.ValueType)
	for k, v := range resource.Schema {
		if v.Required {
			attrRequired[k] = v.Type
		}
	}

	inputAttrs := slices.Clone(cmd.Attrs)
	explicitNames := make(map[string]bool, len(inputAttrs))
	for _, a := range inputAttrs {
		explicitNames[routeros.KebabToSnake(a.Key)] = true
	}

	if cmd.Action == "set" {
		target := cmd.Target
		if target == "" {
			target = "."
		}
		rawTarget := quoteRouterOSValue(target)
		switch resourceName {
		case "routeros_ip_service":
			if !explicitNames["name"] {
				inputAttrs = append(inputAttrs, RawAttr{Key: "name", RawValue: rawTarget})
				explicitNames["name"] = true
			}
			if !explicitNames["numbers"] {
				inputAttrs = append(inputAttrs, RawAttr{Key: "numbers", RawValue: rawTarget})
				explicitNames["numbers"] = true
			}
		case "routeros_routing_bgp_template", "routeros_system_user_group":
			if !explicitNames["name"] {
				inputAttrs = append(inputAttrs, RawAttr{Key: "name", RawValue: rawTarget})
				explicitNames["name"] = true
			}
		}
	}

	// Expand TransformMap from both explicit attributes and [ find ... ] clauses without overwriting explicit keys.
	var transformed []RawAttr
	for _, src := range [][]RawAttr{cmd.Attrs, cmd.FindArgs} {
		for _, a := range src {
			snakeKey := routeros.KebabToSnake(a.Key)
			if targets, ok := TransformMap[resourceName+":"+snakeKey]; ok {
				for _, targetName := range targets {
					if !explicitNames[targetName] {
						transformed = append(transformed, RawAttr{Key: targetName, RawValue: a.RawValue})
						explicitNames[targetName] = true
					}
				}
			}
		}
	}
	inputAttrs = append(inputAttrs, transformed...)

	emitted := make(map[string]bool, len(inputAttrs))
	for _, p := range inputAttrs {
		attrName := routeros.KebabToSnake(p.Key)
		if emitted[attrName] {
			continue
		}

		schemaAttr, ok := resource.Schema[attrName]
		if !ok {
			log.Warnf("Attribute '%v' not found for resource '%v' in provider schema", attrName, resourceName)
			continue
		}

		if schemaAttr.Computed {
			continue
		}

		hclVal, ok := formatSchemaAttributeValue(resourceName, attrName, schemaAttr, p.RawValue)
		if !ok {
			continue
		}

		emitted[attrName] = true
		hclAttributes = append(hclAttributes, fmt.Sprintf("%v = %v", attrName, hclVal))
		parsedAttrs = append(parsedAttrs, ParsedAttr{
			SnakeName:        attrName,
			HCLValue:         hclVal,
			RawRouterOSValue: p.RawValue,
			SchemaType:       schemaAttr.Type,
		})

		if schemaAttr.Required {
			required = append(required, p.Key+"="+p.RawValue)
			delete(attrRequired, attrName)
		}
	}

	if len(attrRequired) > 0 {
		missingKeys := make([]string, 0, len(attrRequired))
		for attrName := range attrRequired {
			missingKeys = append(missingKeys, attrName)
		}
		slices.Sort(missingKeys)

		for _, attrName := range missingKeys {
			attrType := attrRequired[attrName]
			val := placeholderForType(attrType)
			hclAttributes = append(hclAttributes, fmt.Sprintf("%v = %v", attrName, val))
			parsedAttrs = append(parsedAttrs, ParsedAttr{
				SnakeName:        attrName,
				HCLValue:         val,
				RawRouterOSValue: "?",
				SchemaType:       attrType,
				IsPlaceholder:    true,
			})
		}
	}

	return hclAttributes, parsedAttrs, required
}

func formatSchemaAttributeValue(resourceName, attrName string, schemaAttr *schema.Schema, rawValue string) (string, bool) {
	switch schemaAttr.Type {
	case schema.TypeString:
		return formatHCLString(unescapeRouterOSValue(rawValue)), true

	case schema.TypeBool:
		unescaped := unescapeRouterOSValue(rawValue)
		b := routeros.BoolFromMikrotikJSONStr(unescaped)
		if b == "true" || b == "false" {
			return b, true
		}
		return formatHCLString(b), true

	case schema.TypeInt:
		unescaped := unescapeRouterOSValue(rawValue)
		if _, err := strconv.ParseInt(unescaped, 10, 64); err == nil {
			return unescaped, true
		}
		return formatHCLString(unescaped), true

	case schema.TypeFloat:
		unescaped := unescapeRouterOSValue(rawValue)
		if _, err := strconv.ParseFloat(unescaped, 64); err == nil {
			return unescaped, true
		}
		return formatHCLString(unescaped), true

	case schema.TypeSet, schema.TypeList:
		elemSchema, ok := schemaAttr.Elem.(*schema.Schema)
		if !ok {
			log.Warnf("Attribute '%v' for resource '%v' has non-scalar Elem; skipping", attrName, resourceName)
			return "", false
		}
		elems := splitRouterOSList(rawValue)
		if len(elems) == 0 {
			return "[]", true
		}
		formatted := make([]string, 0, len(elems))
		for _, elem := range elems {
			switch elemSchema.Type {
			case schema.TypeString:
				formatted = append(formatted, formatHCLString(elem))
			case schema.TypeBool:
				b := routeros.BoolFromMikrotikJSONStr(elem)
				if b == "true" || b == "false" {
					formatted = append(formatted, b)
				} else {
					formatted = append(formatted, formatHCLString(b))
				}
			case schema.TypeInt:
				if _, err := strconv.ParseInt(elem, 10, 64); err == nil {
					formatted = append(formatted, elem)
				} else {
					formatted = append(formatted, formatHCLString(elem))
				}
			case schema.TypeFloat:
				if _, err := strconv.ParseFloat(elem, 64); err == nil {
					formatted = append(formatted, elem)
				} else {
					formatted = append(formatted, formatHCLString(elem))
				}
			default:
				formatted = append(formatted, formatHCLString(elem))
			}
		}
		return "[" + strings.Join(formatted, ", ") + "]", true

	case schema.TypeMap:
		log.Warnf("Attribute '%v' for resource '%v' has TypeMap; skipping", attrName, resourceName)
		return "", false

	default:
		return formatHCLString(unescapeRouterOSValue(rawValue)), true
	}
}

func placeholderForType(attrType schema.ValueType) string {
	switch attrType {
	case schema.TypeString:
		return formatHCLString("?")
	case schema.TypeBool:
		return "false"
	case schema.TypeInt, schema.TypeFloat:
		return "0"
	case schema.TypeList, schema.TypeSet:
		return "[]"
	case schema.TypeMap:
		return "{}"
	default:
		return formatHCLString("?")
	}
}
