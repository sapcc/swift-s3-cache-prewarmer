// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

// GetCredentialFromMemcache fetches an EC2 credential from Memcache.
// Returns nil if the credential does not exist.
func GetCredentialFromMemcache(mc *memcache.Client, cred CredentialID) *CredentialPayload {
	item, err := mc.Get(cred.CacheKey())
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil
	}
	mustDo("fetch credential from Memcache", err)

	var payload CredentialPayload
	mustDo("decode credential payload from Memcache", json.Unmarshal(item.Value, &payload))
	return &payload
}

// SetCredentialInMemcache writes an EC2 credential into Memcache.
func SetCredentialInMemcache(mc *memcache.Client, cred CredentialID, payload CredentialPayload, expiry time.Duration) {
	buf, err := json.Marshal(payload)
	mustDo("encode credential payload for Memcache", err)

	err = mc.Set(&memcache.Item{
		Key:        cred.CacheKey(),
		Value:      buf,
		Flags:      2, // indicates data type JSON within Swift
		Expiration: int32(expiry.Seconds()),
	})
	mustDo("save credential payload in Memcache", err)
}
