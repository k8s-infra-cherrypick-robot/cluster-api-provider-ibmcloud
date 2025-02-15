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
	gohttp "net/http"
	"strings"

	"github.com/golang-jwt/jwt"

	"github.com/IBM-Cloud/bluemix-go"
	"github.com/IBM-Cloud/bluemix-go/api/resource/resourcev2/controllerv2"
	"github.com/IBM-Cloud/bluemix-go/authentication"
	"github.com/IBM-Cloud/bluemix-go/http"
	"github.com/IBM-Cloud/bluemix-go/rest"
	bxsession "github.com/IBM-Cloud/bluemix-go/session"
	"github.com/IBM/go-sdk-core/v5/core"

	"k8s.io/klog/v2"
)

// Client is used to communicate with IBM Cloud holds the session and user information
type Client struct {
	*bxsession.Session
	User           *User
	ResourceClient controllerv2.ResourceServiceInstanceRepository
}

func authenticateAPIKey(sess *bxsession.Session) error {
	config := sess.Config
	tokenRefresher, err := authentication.NewIAMAuthRepository(config, &rest.Client{
		DefaultHeader: gohttp.Header{
			"User-Agent": []string{http.UserAgent()},
		},
	})
	if err != nil {
		return err
	}
	return tokenRefresher.AuthenticateAPIKey(config.BluemixAPIKey)
}

// User holds the user specific information
type User struct {
	ID         string
	Email      string
	Account    string
	cloudName  string `default:"bluemix"`
	cloudType  string `default:"public"`
	generation int    `default:"2"`
}

func fetchUserDetails(sess *bxsession.Session, generation int) (*User, error) {
	config := sess.Config
	user := User{}
	var bluemixToken string

	if strings.HasPrefix(config.IAMAccessToken, "Bearer") {
		bluemixToken = config.IAMAccessToken[7:len(config.IAMAccessToken)]
	} else {
		bluemixToken = config.IAMAccessToken
	}

	token, err := jwt.Parse(bluemixToken, func(token *jwt.Token) (interface{}, error) {
		return "", nil
	})
	if err != nil && !strings.Contains(err.Error(), "key is of invalid type") {
		return &user, err
	}

	claims := token.Claims.(jwt.MapClaims)
	if email, ok := claims["email"]; ok {
		user.Email = email.(string)
	}
	user.ID = claims["id"].(string)
	user.Account = claims["account"].(map[string]interface{})["bss"].(string)
	iss := claims["iss"].(string)
	if strings.Contains(iss, "https://iam.cloud.ibm.com") {
		user.cloudName = "bluemix"
	} else {
		user.cloudName = "staging"
	}
	user.cloudType = "public"

	user.generation = generation
	return &user, nil
}

// NewClient instantiates and returns an IBM Cloud Client object with session, resource controller and userDetails
func NewClient() *Client {
	c := &Client{}

	authenticator, err := GetAuthenticator()
	if err != nil {
		klog.Fatal(err)
	}
	//TODO: this will be removed once power-go-client migrated to go-sdk-core
	auth, ok := authenticator.(*core.IamAuthenticator)
	if !ok {
		klog.Fatal("failed to assert the authenticator as IAM type, please check the ibm-credentials.env file")
	}
	bxSess, err := bxsession.New(&bluemix.Config{BluemixAPIKey: auth.ApiKey})
	if err != nil {
		klog.Fatal(err)
	}

	c.Session = bxSess

	err = authenticateAPIKey(bxSess)
	if err != nil {
		klog.Fatal(err)
	}

	c.User, err = fetchUserDetails(bxSess, 2)
	if err != nil {
		klog.Fatal(err)
	}

	ctrlv2, err := controllerv2.New(bxSess)
	if err != nil {
		klog.Fatal(err)
	}

	c.ResourceClient = ctrlv2.ResourceServiceInstanceV2()
	return c
}
