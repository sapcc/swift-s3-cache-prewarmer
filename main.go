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
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gophercloud/gophercloud"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/spf13/cobra"
)

var flagConservative bool
var flagExpiryTime time.Duration
var flagPromListenAddress string
var flagMemcacheServers []string

func main() {
	//when behind a mitmproxy, skip certificate validation
	if os.Getenv("HTTPS_PROXY") != "" {
		http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
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
	rootCmd.PersistentFlags().StringSliceVarP(&flagMemcacheServers, "servers", "s", []string{"localhost:11211"}, `List of memcached server endpoints (usually in "host:port" form).`)

	checkKeystoneCmd := cobra.Command{
		Use:   "check-keystone <userid:accesskey>...",
		Short: "Query the given credentials in Keystone (read-only).",
		Long:  "Query the given credentials in Keystone (read-only).",
		Args:  cobra.MinimumNArgs(1),
		Run:   runCheckKeystone,
	}
	rootCmd.AddCommand(&checkKeystoneCmd)

	checkMemcachedCmd := cobra.Command{
		Use:   "check-memcached <userid:accesskey>...",
		Short: "Query the given credentials in Memcache (read-only).",
		Long:  "Query the given credentials in Memcache (read-only).",
		Args:  cobra.MinimumNArgs(1),
		Run:   runCheckMemcache,
	}
	rootCmd.AddCommand(&checkMemcachedCmd)

	prewarmCmd := cobra.Command{
		Use:   "prewarm <userid:accesskey>...",
		Short: "Keep the given credentials prewarmed in Memcache.",
		Long:  "Keep the given credentials prewarmed in Memcache.",
		Args:  cobra.MinimumNArgs(1),
		Run:   runPrewarm,
	}
	prewarmCmd.Flags().BoolVar(&flagConservative, "conservative", false, "Do not touch Memcache when the existing cache entry conflicts with information from Keystone.")
	prewarmCmd.Flags().DurationVar(&flagExpiryTime, "expiry", 10*time.Minute, "Expiration cycle for Memcache entries. The prewarm will happen in intervals of 1/5 the expiration interval.")
	prewarmCmd.Flags().StringVar(&flagPromListenAddress, "listen", "localhost:8080", "Listen address for HTTP server exposing Prometheus metrics.")
	rootCmd.AddCommand(&prewarmCmd)

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
	creds := MustParseCredentials(args)
	identityV3 := MustConnectToKeystone()

	for _, cred := range creds {
		printAsJSON(GetCredentialFromKeystone(identityV3, cred))
	}
}

func runCheckMemcache(cmd *cobra.Command, args []string) {
	creds := MustParseCredentials(args)
	mc := memcache.New(flagMemcacheServers...)

	for _, cred := range creds {
		printAsJSON(GetCredentialFromMemcache(mc, cred))
	}
}

var (
	prewarmTimestampSecsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swift_s3_cache_prewarm_last_run_secs",
			Help: "UNIX timestamp in seconds of last successful cache prewarm for a particular S3 credential.",
		},
		[]string{"userid", "accesskey"},
	)
	prewarmDurationSecsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "swift_s3_cache_prewarm_duration_secs",
			Help: "Duration in seconds of last successful cache prewarm for a particular S3 credential.",
		},
		[]string{"userid", "accesskey"},
	)
)

func runPrewarm(cmd *cobra.Command, args []string) {
	creds := MustParseCredentials(args)
	identityV3 := MustConnectToKeystone()
	mc := memcache.New(flagMemcacheServers...)

	//expose Prometheus metrics
	prometheus.MustRegister(prewarmTimestampSecsGauge)
	prometheus.MustRegister(prewarmDurationSecsGauge)
	ctx := httpext.ContextWithSIGINT(context.Background(), 1*time.Second)
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		err := httpext.ListenAndServeContext(ctx, flagPromListenAddress, nil)
		if err != nil {
			logg.Fatal("error returned from httpext.ListenAndServeContext(): %s", err.Error())
		}
	}()
	//make sure that all swift_s3_cache_prewarm_last_run_secs timeseries exist,
	//even if prewarm never succeeds
	for _, cred := range creds {
		prewarmTimestampSecsGauge.With(cred.AsLabels()).Set(0)
	}

	cycleLength := flagExpiryTime / 5
	tick := time.Tick(cycleLength)

	//do the first prewarm immediately
	doPrewarmCycle(creds, identityV3, mc)

	for {
		select {
		case <-ctx.Done():
			//exit if SIGINT was received
			return
		case <-tick:
			doPrewarmCycle(creds, identityV3, mc)
		}
	}
}

func doPrewarmCycle(creds []CredentialID, identityV3 *gophercloud.ServiceClient, mc *memcache.Client) {
	for _, cred := range creds {
		prewarmStart := time.Now()

		//get new payload from Keystone
		payload := GetCredentialFromKeystone(identityV3, cred)
		if payload == nil {
			//there was a problem getting the payload - we already logged the
			//reason and can directly move on
			continue
		}

		//double-check with Memcache if requested
		if flagConservative {
			cachedPayload := GetCredentialFromMemcache(mc, cred)
			// Accept a not yet cached credential in conservative mode to get it into the cache
			if cachedPayload != nil && !cachedPayload.EqualTo(payload) {
				logg.Info("skipping credential %q: payload in Memcache does not match our expectation", cred.String())
				continue
			}
		}

		//write payload into Memcache (or, if the payload has not changed, just
		//update the expiration time)
		SetCredentialInMemcache(mc, cred, *payload, flagExpiryTime)
		logg.Info("credential %q was prewarmed", cred.String())

		//report Prometheus metrics for this prewarm run
		prewarmEnd := time.Now()
		labels := cred.AsLabels()
		prewarmTimestampSecsGauge.With(labels).Set(float64(prewarmEnd.Unix()))
		prewarmDurationSecsGauge.With(labels).Set(float64(prewarmEnd.Sub(prewarmStart)) / float64(time.Second))
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
