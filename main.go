/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"
)

var cfgCreds CredentialIDList
var cfgMemcacheServers []string

func main() {
	//when behind a mitmproxy, skip certificate validation
	if os.Getenv("HTTPS_PROXY") != "" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
		http.DefaultClient.Transport = http.DefaultTransport
	}

	rootCmd := cobra.Command{
		Use:   "swift-s3-cache-prewarmer",
		Short: "Cache prewarmer for the Swift s3token middleware",
		Long:  "Cache prewarmer for the Swift s3token middleware.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
		},
	}
	rootCmd.PersistentFlags().VarP(&cfgCreds, "credentials", "c", `List of EC2 credentials (comma-separated list of "userid:accesskey" pairs).`)

	checkKeystoneCmd := cobra.Command{
		Use:   "check-keystone",
		Short: "Query the credentials in Keystone (read-only).",
		Long:  "Query the credentials in Keystone (read-only).",
		Args:  cobra.NoArgs,
		Run:   runCheckKeystone,
	}
	rootCmd.AddCommand(&checkKeystoneCmd)

	checkMemcachedCmd := cobra.Command{
		Use:   "check-memcached",
		Short: "Query the credentials in Keystone (read-only).",
		Long:  "Query the credentials in Keystone (read-only).",
		Args:  cobra.NoArgs,
		Run:   runCheckMemcache,
	}
	checkMemcachedCmd.PersistentFlags().StringSliceVarP(&cfgMemcacheServers, "servers", "s", []string{"localhost:11211"}, `List of memcached server endpoints (usually in "host:port" form).`)
	rootCmd.AddCommand(&checkMemcachedCmd)

	if err := rootCmd.Execute(); err != nil {
		//the error was already printed by Execute()
		os.Exit(1)
	}
}

var eo = gophercloud.EndpointOpts{
	Availability: gophercloud.Availability(os.Getenv("OS_INTERFACE")), //defaults to "public" when empty
	Region:       os.Getenv("OS_REGION_NAME"),                         //defaults to empty which is okay
}

func runCheckKeystone(cmd *cobra.Command, args []string) {
	provider, err := clientconfig.AuthenticatedClient(nil)
	must("authenticate to OpenStack using OS_* environment variables", err)
	identityV3, err := openstack.NewIdentityV3(provider, eo)
	must("select OpenStack Identity V3 endpoint", err)

	for _, cred := range cfgCreds {
		payload := GetCredentialFromKeystone(identityV3, cred)
		printAsJSON(payload)
	}
}

func runCheckMemcache(cmd *cobra.Command, args []string) {
	mc := memcache.New(cfgMemcacheServers...)
	for _, cred := range cfgCreds {
		item, err := mc.Get(cred.CacheKey())
		if err == memcache.ErrCacheMiss {
			printAsJSON(nil)
			continue
		}
		must("fetch credential from Memcache", err)

		var payload CredentialPayload
		must("decode credential payload from Memcache", json.Unmarshal(item.Value, &payload))
		printAsJSON(payload)
	}
}

func must(action string, err error) {
	if err != nil {
		logg.Fatal("%s: %v", action, err)
	}
}

func printAsJSON(val interface{}) {
	buf1, err := json.Marshal(val)
	must("serialize to JSON", err)
	var buf2 bytes.Buffer
	must("pretty-print JSON", json.Indent(&buf2, buf1, "", "  "))
	fmt.Println(buf2.String())
}
