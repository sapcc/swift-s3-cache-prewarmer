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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud/openstack/identity/v3/tokens"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
)

//CredentialID identifies an EC2 credential.
type CredentialID struct {
	UserID    string
	AccessKey string
}

//String recovers the string representation of this CredentialID.
func (cred CredentialID) String() string {
	return fmt.Sprintf("%s:%s", cred.UserID, cred.AccessKey)
}

//CacheKey returns the key under which this credential's payload is stored in memcache.
func (cred CredentialID) CacheKey() string {
	rawKey := "s3secret/" + cred.AccessKey
	hashBytes := md5.Sum([]byte(rawKey))
	return hex.EncodeToString(hashBytes[:])
}

//AsLabels represents this credential as a set of Prometheus labels.
func (cred CredentialID) AsLabels() prometheus.Labels {
	return prometheus.Labels{
		"userid":    cred.UserID,
		"accesskey": cred.AccessKey,
	}
}

//MustParseCredentials parses CredentialIDs passed in as CLI arguments.
func MustParseCredentials(args []string) []CredentialID {
	result := make([]CredentialID, len(args))
	for idx, arg := range args {
		fields := strings.Split(arg, ":")
		if len(fields) != 2 {
			logg.Fatal("cannot parse userid:accesskey pair: %q", arg)
		}
		cred := CredentialID{
			UserID:    strings.TrimSpace(fields[0]),
			AccessKey: strings.TrimSpace(fields[1]),
		}
		if cred.UserID == "" || cred.AccessKey == "" {
			logg.Fatal("cannot parse userid:accesskey pair: %q", arg)
		}
		result[idx] = cred
	}
	return result
}

//CredentialPayload contains the payload for a credential which we write into memcached.
type CredentialPayload struct {
	Headers map[string]string
	Project tokens.Project
	Secret  string
}

//MarshalJSON implements the json.Marshaler interface.
func (p CredentialPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal([]interface{}{p.Headers, p.Project, p.Secret})
}

//UnmarshalJSON implements the json.Marshaler interface.
func (p *CredentialPayload) UnmarshalJSON(buf []byte) error {
	var fields []json.RawMessage
	err := json.Unmarshal(buf, &fields)
	if err != nil {
		return err
	}
	if len(fields) != 3 {
		return fmt.Errorf("expected CredentialPayload with 3 elements, got %d elements", len(fields))
	}

	err = json.Unmarshal([]byte(fields[0]), &p.Headers)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(fields[1]), &p.Project)
	if err != nil {
		return err
	}
	err = json.Unmarshal([]byte(fields[2]), &p.Secret)
	if err != nil {
		return err
	}
	return nil
}
