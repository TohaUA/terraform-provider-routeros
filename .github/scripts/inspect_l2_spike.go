// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-routeros/routeros/v3"
)

func main() {
	host := os.Getenv("ROS_IP_ADDRESS")
	if host == "" {
		host = "127.0.0.1"
	}
	user := os.Getenv("ROS_USERNAME")
	if user == "" {
		user = "admin"
	}
	pass := os.Getenv("ROS_PASSWORD")

	var client *routeros.Client
	var err error
	for i := 0; i < 24; i++ {
		client, err = routeros.Dial(host+":8728", user, pass)
		if err == nil {
			break
		}
		time.Sleep(5 * time.Second)
	}
	if err != nil {
		log.Fatalf("API dial failed: %v", err)
	}
	defer client.Close()

	phase := "pre"
	if len(os.Args) > 1 {
		phase = os.Args[1]
	}

	queries := []struct {
		title string
		path  string
	}{
		{"CHR 7.25.2 interfaces", "/interface/print"},
		{"CHR 7.25.2 IPv4 addresses", "/ip/address/print"},
		{"CHR 7.25.2 IPv6 addresses", "/ipv6/address/print"},
		{"CHR 7.25.2 mac-server", "/tool/mac-server/print"},
		{"CHR 7.25.2 mac-winbox", "/tool/mac-server/mac-winbox/print"},
		{"CHR 7.25.2 neighbor discovery", "/ip/neighbor/discovery-settings/print"},
		{"CHR 7.25.2 ip services", "/ip/service/print"},
	}

	for _, q := range queries {
		fmt.Printf("=== SPIKE (%s): %s ===\n", phase, q.title)
		res, err := client.Run(q.path)
		if err != nil {
			fmt.Printf("  error: %v\n", err)
			continue
		}
		for _, re := range res.Re {
			fmt.Printf("  %v\n", re.Map)
		}
	}
}
