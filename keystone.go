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
	"fmt"
	"strings"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/ec2credentials"
	"github.com/gophercloud/gophercloud/openstack/identity/v3/extensions/ec2tokens"
	"github.com/sapcc/go-bits/logg"
)

//GetCredentialFromKeystone fetches an EC2 credential from Keystone.
//Returns nil if the credential does not exist.
func GetCredentialFromKeystone(identityV3 *gophercloud.ServiceClient, cred CredentialID) *CredentialPayload {
	//get secret from Keystone
	credInfo, err := ec2credentials.Get(identityV3, cred.UserID, cred.AccessKey).Extract()
	if _, ok := err.(gophercloud.ErrDefault404); ok {
		logg.Info("skipping credential %q: not found in Keystone", cred.String())
		return nil
	}
	must(fmt.Sprintf(`lookup EC2 credential %q in Keystone`, cred.String()), err)

	//login with this credential to get further information
	result := ec2tokens.Create(identityV3, &ec2tokens.AuthOptions{
		Access: cred.AccessKey,
		Secret: credInfo.Secret,
	})
	if _, ok := err.(gophercloud.ErrDefault401); ok {
		logg.Info("skipping credential %q: authorization failed", cred.String())
		return nil
	}
	must(fmt.Sprintf(`login as EC2 credential %q in Keystone`, cred.String()), err)

	//convert into the payload format used by the token cache
	project, err := result.ExtractProject()
	must(fmt.Sprintf("extract project data for EC2 credential %q", cred.String()), err)
	user, err := result.ExtractUser()
	must(fmt.Sprintf("extract user data for EC2 credential %q", cred.String()), err)
	roles, err := result.ExtractRoles()
	must(fmt.Sprintf("extract role data for EC2 credential %q", cred.String()), err)
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
