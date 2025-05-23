// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/ec2credentials"
	"github.com/gophercloud/gophercloud/v2/openstack/identity/v3/ec2tokens"
	"github.com/sapcc/go-bits/gophercloudext"
	"github.com/sapcc/go-bits/logg"
)

// MustConnectToKeystone connects to Keystone or dies trying.
func MustConnectToKeystone(ctx context.Context) *gophercloud.ServiceClient {
	provider, eo, err := gophercloudext.NewProviderClient(ctx, nil)
	mustDo("authenticate to OpenStack", err)
	identityV3, err := openstack.NewIdentityV3(provider, eo)
	mustDo("select OpenStack Identity V3 endpoint", err)
	return identityV3
}

// GetCredentialFromKeystone fetches an EC2 credential from Keystone.
// Returns nil if the credential does not exist.
func GetCredentialFromKeystone(ctx context.Context, identityV3 *gophercloud.ServiceClient, cred CredentialID) *CredentialPayload {
	// get secret from Keystone
	credInfo, err := ec2credentials.Get(ctx, identityV3, cred.UserID, cred.AccessKey).Extract()
	if gophercloud.ResponseCodeIs(err, http.StatusNotFound) {
		logg.Info("skipping credential %q: not found in Keystone", cred.String())
		return nil
	}
	mustDo(fmt.Sprintf(`lookup EC2 credential %q in Keystone`, cred.String()), err)

	// login with this credential to get further information
	result := ec2tokens.Create(ctx, identityV3, &ec2tokens.AuthOptions{
		Access: cred.AccessKey,
		Secret: credInfo.Secret,
	})
	if gophercloud.ResponseCodeIs(err, http.StatusUnauthorized) {
		logg.Info("skipping credential %q: authorization failed", cred.String())
		return nil
	}
	mustDo(fmt.Sprintf(`login as EC2 credential %q in Keystone`, cred.String()), err)

	// convert into the payload format used by the token cache
	project, err := result.ExtractProject()
	mustDo(fmt.Sprintf("extract project data for EC2 credential %q", cred.String()), err)
	user, err := result.ExtractUser()
	mustDo(fmt.Sprintf("extract user data for EC2 credential %q", cred.String()), err)
	roles, err := result.ExtractRoles()
	mustDo(fmt.Sprintf("extract role data for EC2 credential %q", cred.String()), err)
	roleNames := make([]string, len(roles))
	for idx, role := range roles {
		roleNames[idx] = role.Name
	}

	return &CredentialPayload{
		Headers: map[string]string{
			"X-Identity-Status":     "Confirmed",
			"X-Roles":               strings.Join(roleNames, ","),
			"X-User-Id":             user.ID,
			"X-User-Name":           user.Name,
			"X-User-Domain-Id":      user.Domain.ID,
			"X-User-Domain-Name":    user.Domain.Name,
			"X-Tenant-Id":           project.ID,
			"X-Tenant-Name":         project.Name,
			"X-Project-Id":          project.ID,
			"X-Project-Name":        project.Name,
			"X-Project-Domain-Id":   project.Domain.ID,
			"X-Project-Domain-Name": project.Domain.Name,
		},
		Project: *project,
		Secret:  credInfo.Secret,
	}
}

func mustDo(action string, err error) {
	if err != nil {
		logg.Fatal("%s: %v", action, err)
	}
}
