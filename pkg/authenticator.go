/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pkg

import (
	"fmt"

	"github.com/IBM/go-sdk-core/v5/core"
)

// This expects the credential file in the following search order:
// 1) ${IBM_CREDENTIALS_FILE}
// 2) <user-home-dir>/ibm-credentials.env
// 3) <current-working-directory>/ibm-credentials.env
//
// and the format is:
// $ cat ibm-credentials.env
// IBMCLOUD_AUTH_TYPE=iam
// IBMCLOUD_APIKEY=xxxxxxxxxxxxx
// IBMCLOUD_AUTH_URL=https://iam.cloud.ibm.com

const (
	serviceIBMCloud = "IBMCLOUD"
)

// GetAuthenticator instantiates an Authenticator from external config file
func GetAuthenticator() (core.Authenticator, error) {
	auth, err := core.GetAuthenticatorFromEnvironment(serviceIBMCloud)
	if err != nil {
		return nil, err
	}
	switch auth.(type) {
	case *core.IamAuthenticator:
		return auth, nil
	default:
		return nil, fmt.Errorf("only IAM authenticator is supported")
	}
}
