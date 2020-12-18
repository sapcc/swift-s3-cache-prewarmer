# swift-s3-cache-prewarmer

We have a few customers making extensive use of the S3 API in our [Swift](https://github.com/openstack/swift) cluster.
To avoid putting too much load on Keystone, Swift can cache S3 credentials in a Memcache. However, when the customer
traffic is high enough to keep multiple parallel Swift API workers busy, once a cache entry expires, they all hit
Keystone simultaneously. To avoid this high request load on Keystone caused by expiring cache entries, this tool
periodically refreshes the cache entries so that they never expire.

## Usage

Build with `make/make install` or `go get` or `docker build`. Check out the `--help` for how to run this.

## TODO

- Prometheus metric: last successful prewarm time per credential, time spent prewarming per credential
