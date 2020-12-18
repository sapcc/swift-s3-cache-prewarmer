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

//CredentialIDList is a []CredentialID that implements the cobra.Value interface.
type CredentialIDList []CredentialID

//String implements the cobra.Value interface.
func (l *CredentialIDList) String() string {
	strs := make([]string, len(*l))
	for idx, cred := range *l {
		strs[idx] = cred.String()
	}
	return strings.Join(strs, ",")
}

//Set implements the cobra.Value interface.
func (l *CredentialIDList) Set(input string) error {
	*l = nil
	for _, pair := range strings.Split(input, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		fields := strings.Split(pair, ":")
		if len(fields) != 2 {
			return fmt.Errorf(`cannot parse userid:accesskey pair: %q`, pair)
		}
		cred := CredentialID{
			UserID:    strings.TrimSpace(fields[0]),
			AccessKey: strings.TrimSpace(fields[1]),
		}
		if cred.UserID == "" || cred.AccessKey == "" {
			return fmt.Errorf(`cannot parse userid:accesskey pair: %q`, pair)
		}
		*l = append(*l, cred)
	}
	return nil
}

//Type implements the cobra.Value interface.
func (l *CredentialIDList) Type() string {
	return "<credentials>"
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
