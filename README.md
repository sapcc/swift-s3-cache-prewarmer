<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
SPDX-License-Identifier: Apache-2.0
-->

# swift-s3-cache-prewarmer

[![CI](https://github.com/sapcc/swift-s3-cache-prewarmer/actions/workflows/ci.yaml/badge.svg)](https://github.com/sapcc/swift-s3-cache-prewarmer/actions/workflows/ci.yaml)

We have a few customers making extensive use of the S3 API in our [Swift](https://github.com/openstack/swift) cluster.
To avoid putting too much load on Keystone, Swift can cache S3 credentials in a Memcache. However, when the customer
traffic is high enough to keep multiple parallel Swift API workers busy, once a cache entry expires, they all hit
Keystone simultaneously. To avoid this high request load on Keystone caused by expiring cache entries, this tool
periodically refreshes the cache entries so that they never expire.

## Usage

Build with `make/make install` or `go get` or `docker build`. Check out the `--help` for how to run this.

## Metrics

The `prewarm` command exposes two gauges for each credential that was prewarmed:

- `swift_s3_cache_prewarm_last_run_secs`: UNIX timestamp in seconds of last successful cache prewarm (or 0 before the first successful prewarm)
- `swift_s3_cache_prewarm_duration_secs`: duration in seconds of last successful cache prewarm (or absent before the first successful prewarm)

Each time series has the labels `userid` and `accesskey` identifying the credential in question.
