// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/gophercloud/gophercloud/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sapcc/go-api-declarations/bininfo"
	"github.com/sapcc/go-bits/httpext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
	"github.com/sapcc/go-bits/osext"
	"github.com/spf13/cobra"
	"go.uber.org/automaxprocs/maxprocs"
)

var flagConservative bool
var flagExpiryTime time.Duration
var flagPromListenAddress string
var flagMemcacheServers []string

func main() {
	logg.ShowDebug = osext.GetenvBool("SWIFT_S3CP_DEBUG")
	undoMaxprocs := must.Return(maxprocs.Set(maxprocs.Logger(logg.Debug)))
	defer undoMaxprocs()

	wrap := httpext.WrapTransport(&http.DefaultTransport)
	wrap.SetInsecureSkipVerify(os.Getenv("HTTPS_PROXY") != "") // skip cert validation when behind mitmproxy (DO NOT SET IN PRODUCTION)
	wrap.SetOverrideUserAgent(bininfo.Component(), bininfo.VersionOr("rolling"))

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

	ctx := httpext.ContextWithSIGINT(context.Background(), 1*time.Second)
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		// the error was already printed by Execute()
		os.Exit(1) //nolint:gocritic // undomacprocs is not critical to be run
	}
}

func runCheckKeystone(cmd *cobra.Command, args []string) {
	creds := MustParseCredentials(args)
	identityV3 := MustConnectToKeystone(cmd.Context())

	for _, cred := range creds {
		printAsJSON(GetCredentialFromKeystone(cmd.Context(), identityV3, cred))
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
	ctx := cmd.Context()
	creds := MustParseCredentials(args)
	identityV3 := MustConnectToKeystone(ctx)
	mc := memcache.New(flagMemcacheServers...)

	// expose Prometheus metrics
	prometheus.MustRegister(prewarmTimestampSecsGauge)
	prometheus.MustRegister(prewarmDurationSecsGauge)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		must.Succeed(httpext.ListenAndServeContext(ctx, flagPromListenAddress, mux))
	}()
	// make sure that all swift_s3_cache_prewarm_last_run_secs timeseries exist,
	// even if prewarm never succeeds
	for _, cred := range creds {
		prewarmTimestampSecsGauge.With(cred.AsLabels()).Set(0)
	}

	cycleLength := flagExpiryTime / 5
	tick := time.Tick(cycleLength)

	// do the first prewarm immediately
	doPrewarmCycle(ctx, creds, identityV3, mc)

	for {
		select {
		case <-ctx.Done():
			// exit if SIGINT was received
			return
		case <-tick:
			doPrewarmCycle(ctx, creds, identityV3, mc)
		}
	}
}

func doPrewarmCycle(ctx context.Context, creds []CredentialID, identityV3 *gophercloud.ServiceClient, mc *memcache.Client) {
	for _, cred := range creds {
		prewarmStart := time.Now()

		// get new payload from Keystone
		payload := GetCredentialFromKeystone(ctx, identityV3, cred)
		if payload == nil {
			// there was a problem getting the payload - we already logged the
			// reason and can directly move on
			continue
		}

		// double-check with Memcache if requested
		if flagConservative {
			cachedPayload := GetCredentialFromMemcache(mc, cred)
			// Accept a not yet cached credential in conservative mode to get it into the cache
			if cachedPayload != nil && !cachedPayload.EqualTo(payload) {
				logg.Info("skipping credential %q: payload in Memcache does not match our expectation", cred.String())
				continue
			}
		}

		// write payload into Memcache (or, if the payload has not changed, just
		// update the expiration time)
		SetCredentialInMemcache(mc, cred, *payload, flagExpiryTime)
		logg.Info("credential %q was prewarmed", cred.String())

		// report Prometheus metrics for this prewarm run
		prewarmEnd := time.Now()
		labels := cred.AsLabels()
		prewarmTimestampSecsGauge.With(labels).Set(float64(prewarmEnd.Unix()))
		prewarmDurationSecsGauge.With(labels).Set(float64(prewarmEnd.Sub(prewarmStart)) / float64(time.Second))
	}
}

func printAsJSON(val any) {
	buf := must.Return(json.MarshalIndent(val, "", "  "))
	fmt.Println(string(buf))
}
