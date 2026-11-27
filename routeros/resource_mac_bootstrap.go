// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package routeros

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/terraform-routeros/terraform-provider-routeros/internal/macboot"
)

var macBootstrapFunc = macboot.Bootstrap

// ResourceMacBootstrap provisions initial IPv4 and TLS REST/API access on an
// unaddressed RouterOS device over layer-2 MAC-Telnet (UDP/20561).
func ResourceMacBootstrap() *schema.Resource {
	resSchema := map[string]*schema.Schema{
		MetaId: {
			Type:     schema.TypeInt,
			Optional: true,
			ForceNew: true,
			Default:  int(Name),
			DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
				return true
			},
		},
		MetaResourcePath: {
			Type:     schema.TypeString,
			Optional: true,
			ForceNew: true,
			Default:  "local",
			DiffSuppressFunc: func(k, old, new string, d *schema.ResourceData) bool {
				return true
			},
		},
		"interface": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "Local host network interface connected to the target layer-2 segment.",
		},
		"mac_address": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "Target RouterOS interface MAC address.",
		},
		"address": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "IPv4 CIDR address to assign on `target_interface` (for example `192.168.88.1/24`).",
		},
		"target_interface": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "RouterOS interface name to assign `address` to (for example `ether2` or `bridge`).",
		},
		"username": {
			Type:        schema.TypeString,
			Required:    true,
			ForceNew:    true,
			Description: "RouterOS username used to authenticate the MAC-Telnet session.",
		},
		"password_wo": {
			Type:          schema.TypeString,
			Optional:      true,
			Sensitive:     true,
			WriteOnly:     true,
			ConflictsWith: []string{"password"},
			Description:   "Write-only RouterOS password for MAC-Telnet authentication (Terraform 1.11+, omitted from state and plans). Increment `password_wo_version` to force replacement.",
		},
		"password_wo_version": {
			Type:        schema.TypeInt,
			Optional:    true,
			ForceNew:    true,
			Description: "Increment this value to force replacement when updating `password_wo`.",
		},
		"password": {
			Type:          schema.TypeString,
			Optional:      true,
			Sensitive:     true,
			ForceNew:      true,
			ConflictsWith: []string{"password_wo"},
			Description:   "Legacy RouterOS password for MAC-Telnet authentication (stored in state; prefer `password_wo`).",
		},
		"allow_insecure_l2": {
			Type:        schema.TypeBool,
			Required:    true,
			ForceNew:    true,
			Description: "Explicit acknowledgement that post-login MAC-Telnet traffic is cleartext with no server authentication. Must be `true` to run bootstrap.",
		},
		"expected_tls_fingerprint": {
			Type:        schema.TypeString,
			Optional:    true,
			ForceNew:    true,
			Description: "Optional expected SHA-256 leaf certificate fingerprint obtained out-of-band. When set, bootstrap fails closed if the router returns a different fingerprint.",
		},
		"certificate_name": {
			Type:        schema.TypeString,
			Optional:    true,
			Default:     macboot.DefaultCertificateName,
			ForceNew:    true,
			Description: "Name of the self-signed TLS server certificate created on RouterOS if absent.",
		},
		"enable_api_ssl": {
			Type:        schema.TypeBool,
			Optional:    true,
			Default:     false,
			ForceNew:    true,
			Description: "Whether to also enable `/ip/service` `api-ssl` with the bootstrap certificate.",
		},
		"timeout": {
			Type:        schema.TypeString,
			Optional:    true,
			Default:     "30s",
			ForceNew:    true,
			Description: "Bootstrap timeout as a Go duration string (for example `30s`).",
		},
		"tls_fingerprint": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Normalized uppercase SHA-256 fingerprint of the router's verified TLS certificate.",
		},
		"router_address": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "Bootstrapped IPv4 host address (without CIDR prefix) verified over pinned HTTPS REST.",
		},
		"identity": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "RouterOS system identity reported by the bootstrapped router.",
		},
		"routeros_version": {
			Type:        schema.TypeString,
			Computed:    true,
			Description: "RouterOS version string reported by the bootstrapped router.",
		},
	}

	return &schema.Resource{
		Description: "Bootstraps initial IPv4 addressing and HTTPS REST (`www-ssl`) TLS access on an unaddressed MikroTik RouterOS device over layer-2 MAC-Telnet, then verifies `/rest/system/resource` over pinned TLS. Intended only for Stage 1 initial provisioning on trusted, isolated segments.",
		CustomizeDiff: func(_ context.Context, d *schema.ResourceDiff, _ interface{}) error {
			allow, ok := d.Get("allow_insecure_l2").(bool)
			if !ok || !allow {
				return fmt.Errorf("%v: set allow_insecure_l2 = true only on a trusted, isolated layer-2 segment", macboot.ErrInsecureL2Required)
			}
			if rawMAC, ok := d.Get("mac_address").(string); ok && rawMAC != "" {
				hw, err := net.ParseMAC(strings.TrimSpace(rawMAC))
				if err != nil || len(hw) != 6 {
					return fmt.Errorf("invalid mac_address %q: expected 6-byte MAC address", rawMAC)
				}
			}
			if rawAddr, ok := d.Get("address").(string); ok && rawAddr != "" {
				ip, _, err := net.ParseCIDR(strings.TrimSpace(rawAddr))
				if err != nil || ip.To4() == nil {
					return fmt.Errorf("invalid address %q: expected IPv4 CIDR (for example 192.168.88.1/24)", rawAddr)
				}
			}
			if rawTimeout, ok := d.Get("timeout").(string); ok && rawTimeout != "" {
				dur, err := time.ParseDuration(rawTimeout)
				if err != nil || dur <= 0 {
					return fmt.Errorf("invalid timeout %q: must be a positive Go duration", rawTimeout)
				}
			}
			if rawFP, ok := d.Get("expected_tls_fingerprint").(string); ok && strings.TrimSpace(rawFP) != "" {
				if _, err := macboot.NormalizeFingerprint(rawFP); err != nil {
					return fmt.Errorf("invalid expected_tls_fingerprint: %w", err)
				}
			}
			return nil
		},
		CreateContext: resourceMacBootstrapCreate,
		ReadContext: func(_ context.Context, _ *schema.ResourceData, _ interface{}) diag.Diagnostics {
			return nil
		},
		DeleteContext: func(_ context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
			d.SetId("")
			return nil
		},
		Schema: resSchema,
	}
}

func resourceMacBootstrapCreate(ctx context.Context, d *schema.ResourceData, _ interface{}) diag.Diagnostics {
	var diags diag.Diagnostics

	if !d.Get("allow_insecure_l2").(bool) {
		return diag.Errorf("%v", macboot.ErrInsecureL2Required)
	}

	timeout, err := time.ParseDuration(d.Get("timeout").(string))
	if err != nil || timeout <= 0 {
		return diag.Errorf("invalid timeout %q: must be a positive duration", d.Get("timeout").(string))
	}

	password := d.Get("password").(string)
	if v, rawDiags := d.GetRawConfigAt(cty.GetAttrPath("password_wo")); !rawDiags.HasError() && !v.IsNull() && v.IsKnown() && v.Type() == cty.String {
		password = v.AsString()
	}

	expectedFP := strings.TrimSpace(d.Get("expected_tls_fingerprint").(string))

	diags = append(diags, diag.Diagnostic{
		Severity: diag.Warning,
		Summary:  "Layer-2 MAC-Telnet bootstrap uses cleartext transport after login",
		Detail:   "MAC-Telnet protects credentials with EC-SRP5 during login, but all subsequent CLI commands and output are sent in cleartext with no server authentication. Run only on trusted, isolated segments and disable MAC servers in Stage 2.",
	})
	if expectedFP == "" {
		diags = append(diags, diag.Diagnostic{
			Severity: diag.Warning,
			Summary:  "TLS fingerprint learned via unauthenticated layer-2 TOFU",
			Detail:   "expected_tls_fingerprint is unset. Rotate the router admin password over pinned HTTPS/API-SSL in Stage 2 and pin tls_fingerprint afterwards.",
		})
	}

	cfg := macboot.BootstrapConfig{
		Interface:              d.Get("interface").(string),
		MACAddress:             d.Get("mac_address").(string),
		Address:                d.Get("address").(string),
		TargetInterface:        d.Get("target_interface").(string),
		Username:               d.Get("username").(string),
		Password:               password,
		AllowInsecureL2:        true,
		ExpectedTLSFingerprint: expectedFP,
		CertificateName:        d.Get("certificate_name").(string),
		EnableAPISSL:           d.Get("enable_api_ssl").(bool),
		Timeout:                timeout,
	}

	res, err := macBootstrapFunc(ctx, cfg)
	if err != nil {
		return append(diags, diag.FromErr(err)...)
	}

	d.SetId(strings.ToUpper(cfg.MACAddress) + "/" + res.RouterAddress)
	for k, v := range map[string]interface{}{
		"tls_fingerprint":  res.TLSFingerprint,
		"router_address":   res.RouterAddress,
		"identity":         res.Identity,
		"routeros_version": res.RouterOSVersion,
	} {
		if err := d.Set(k, v); err != nil {
			return append(diags, diag.FromErr(err)...)
		}
	}

	return diags
}
