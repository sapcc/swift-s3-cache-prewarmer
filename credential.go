// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/md5" //nolint:gosec // used in a none security relevant way
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"maps"

	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/tokens"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-bits/logg"
)

// CredentialID identifies an EC2 credential.
type CredentialID struct {
	UserID    string
	AccessKey string
}

// String recovers the string representation of this CredentialID.
func (cred CredentialID) String() string {
	return fmt.Sprintf("%s:%s", cred.UserID, cred.AccessKey)
}

// CacheKey returns the key under which this credential's payload is stored in memcache.
func (cred CredentialID) CacheKey() string {
	rawKey := "s3secret/" + cred.AccessKey
	hashBytes := md5.Sum([]byte(rawKey)) //nolint:gosec // input cannot be chosen by the user
	return hex.EncodeToString(hashBytes[:])
}

// AsLabels represents this credential as a set of Prometheus labels.
func (cred CredentialID) AsLabels() prometheus.Labels {
	return prometheus.Labels{
		"userid":    cred.UserID,
		"accesskey": cred.AccessKey,
	}
}

// MustParseCredentials parses CredentialIDs passed in as CLI arguments.
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

// CredentialPayload contains the payload for a credential which we write into memcached.
type CredentialPayload struct {
	Headers map[string]string
	Project tokens.Project
	Secret  string
}

// MarshalJSON implements the json.Marshaler interface.
func (p CredentialPayload) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{p.Headers, p.Project, p.Secret})
}

// UnmarshalJSON implements the json.Marshaler interface.
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

// EqualTo is similar to reflect.DeepEqual(), but considers some additional invariants.
func (p *CredentialPayload) EqualTo(other *CredentialPayload) bool {
	if (p == nil) != (other == nil) {
		return false
	}
	if p == nil {
		// therefore also `other == nil`
		return true
	}
	// Now we know that `p != nil && other != nil`.

	// make a deep copy of the RHS
	rhs := &CredentialPayload{
		Headers: make(map[string]string, len(other.Headers)),
		Project: other.Project,
		Secret:  other.Secret,
	}
	maps.Copy(rhs.Headers, other.Headers)

	// the one thing that makes this function different from a plain
	// reflect.DeepEqual(): Headers["X-Roles"] is a comma-separated list
	// where ordering does not matter
	lhsRoles := p.Headers["X-Roles"]
	rhsRoles := rhs.Headers["X-Roles"]
	if lhsRoles != "" && rhsRoles != "" {
		rhs.Headers["X-Roles"] = sortCommaSeparatedLikeInReference(rhsRoles, lhsRoles)
	}

	return reflect.DeepEqual(p, rhs)
}

func sortCommaSeparatedLikeInReference(input, reference string) string {
	refFieldIndex := make(map[string]int)
	for idx, field := range strings.Split(reference, ",") {
		refFieldIndex[field] = idx
	}

	fields := strings.Split(input, ",")
	sort.Slice(fields, func(i, j int) bool {
		idx1 := refFieldIndex[fields[i]]
		idx2 := refFieldIndex[fields[j]]
		if idx1 == idx2 {
			return fields[i] < fields[j]
		}
		return idx1 < idx2
	})
	return strings.Join(fields, ",")
}
