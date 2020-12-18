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
	"encoding/json"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

//GetCredentialFromMemcache fetches an EC2 credential from Memcache.
//Returns nil if the credential does not exist.
func GetCredentialFromMemcache(mc *memcache.Client, cred CredentialID) *CredentialPayload {
	item, err := mc.Get(cred.CacheKey())
	if err == memcache.ErrCacheMiss {
		return nil
	}
	must("fetch credential from Memcache", err)

	var payload CredentialPayload
	must("decode credential payload from Memcache", json.Unmarshal(item.Value, &payload))
	return &payload
}

//SetCredentialInMemcache writes an EC2 credential into Memcache.
func SetCredentialInMemcache(mc *memcache.Client, cred CredentialID, payload CredentialPayload, expiry time.Duration) {
	buf, err := json.Marshal(payload)
	must("encode credential payload for Memcache", err)

	err = mc.Set(&memcache.Item{
		Key:        cred.CacheKey(),
		Value:      buf,
		Flags:      2, //indicates data type JSON within Swift
		Expiration: int32(expiry.Seconds()),
	})
	must("save credential payload in Memcache", err)
}
