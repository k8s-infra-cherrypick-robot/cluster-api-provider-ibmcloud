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

package scope

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/vpc-go-sdk/vpcv1"

	"k8s.io/klog/v2/klogr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-ibmcloud/api/v1alpha3"
)

// ClusterScopeParams defines the input parameters used to create a new ClusterScope.
type ClusterScopeParams struct {
	IBMVPCClients
	Client        client.Client
	Logger        logr.Logger
	Cluster       *clusterv1.Cluster
	IBMVPCCluster *infrav1.IBMVPCCluster
}

// ClusterScope defines a scope defined around a cluster.
type ClusterScope struct {
	logr.Logger
	client      client.Client
	patchHelper *patch.Helper

	IBMVPCClients
	Cluster       *clusterv1.Cluster
	IBMVPCCluster *infrav1.IBMVPCCluster
}

// NewClusterScope creates a new ClusterScope from the supplied parameters.
func NewClusterScope(params ClusterScopeParams, authenticator core.Authenticator, svcEndpoint string) (*ClusterScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.IBMVPCCluster == nil {
		return nil, errors.New("failed to generate new scope from nil IBMVPCCluster")
	}

	if params.Logger == nil {
		params.Logger = klogr.New()
	}

	helper, err := patch.NewHelper(params.IBMVPCCluster, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	vpcErr := params.IBMVPCClients.setIBMVPCService(authenticator, svcEndpoint)
	if vpcErr != nil {
		return nil, errors.Wrap(vpcErr, "failed to create IBM VPC session")
	}

	return &ClusterScope{
		Logger:        params.Logger,
		client:        params.Client,
		IBMVPCClients: params.IBMVPCClients,
		Cluster:       params.Cluster,
		IBMVPCCluster: params.IBMVPCCluster,
		patchHelper:   helper,
	}, nil
}

// CreateVPC creates a new IBM VPC in specified resource group
func (s *ClusterScope) CreateVPC() (*vpcv1.VPC, error) {
	vpcReply, err := s.ensureVPCUnique(s.IBMVPCCluster.Spec.VPC)
	if err != nil {
		return nil, err
	} else if vpcReply != nil {
		//TODO need a reasonable wrapped error
		return vpcReply, nil
	}

	options := &vpcv1.CreateVPCOptions{}
	options.SetResourceGroup(&vpcv1.ResourceGroupIdentity{
		ID: &s.IBMVPCCluster.Spec.ResourceGroup,
	})
	options.SetName(s.IBMVPCCluster.Spec.VPC)
	vpc, _, err := s.IBMVPCClients.VPCService.CreateVPC(options)
	if err != nil {
		return nil, err
	} else if err := s.updateDefaultSG(*vpc.DefaultSecurityGroup.ID); err != nil {
		return nil, err
	}
	return vpc, nil
}

// DeleteVPC deletes IBM VPC associated with a VPC id
func (s *ClusterScope) DeleteVPC() error {
	deleteVpcOptions := &vpcv1.DeleteVPCOptions{}
	deleteVpcOptions.SetID(s.IBMVPCCluster.Status.VPC.ID)
	_, err := s.IBMVPCClients.VPCService.DeleteVPC(deleteVpcOptions)

	return err
}

func (s *ClusterScope) ensureVPCUnique(vpcName string) (*vpcv1.VPC, error) {
	listVpcsOptions := &vpcv1.ListVpcsOptions{}
	vpcs, _, err := s.IBMVPCClients.VPCService.ListVpcs(listVpcsOptions)
	if err != nil {
		return nil, err
	}
	for _, vpc := range vpcs.Vpcs {
		if (*vpc.Name) == vpcName {
			return &vpc, nil
		}
	}
	return nil, nil
}

func (s *ClusterScope) updateDefaultSG(sgID string) error {
	options := &vpcv1.CreateSecurityGroupRuleOptions{}
	options.SetSecurityGroupID(sgID)
	options.SetSecurityGroupRulePrototype(&vpcv1.SecurityGroupRulePrototype{
		Direction: core.StringPtr("inbound"),
		Protocol:  core.StringPtr("all"),
		IPVersion: core.StringPtr("ipv4"),
	})
	_, _, err := s.IBMVPCClients.VPCService.CreateSecurityGroupRule(options)
	return err
}

// ReserveFIP creates a Floating IP in a provided resource group and zone
func (s *ClusterScope) ReserveFIP() (*vpcv1.FloatingIP, error) {
	fipName := s.IBMVPCCluster.Name + "-control-plane"
	fipReply, err := s.ensureFIPUnique(fipName)
	if err != nil {
		return nil, err
	} else if fipReply != nil {
		//TODO need a reasonable wrapped error
		return fipReply, nil
	}
	options := &vpcv1.CreateFloatingIPOptions{}

	options.SetFloatingIPPrototype(&vpcv1.FloatingIPPrototype{
		Name: &fipName,
		ResourceGroup: &vpcv1.ResourceGroupIdentity{
			ID: &s.IBMVPCCluster.Spec.ResourceGroup,
		},
		Zone: &vpcv1.ZoneIdentity{
			Name: &s.IBMVPCCluster.Spec.Zone,
		},
	})

	floatingIP, _, err := s.IBMVPCClients.VPCService.CreateFloatingIP(options)
	return floatingIP, err
}

func (s *ClusterScope) ensureFIPUnique(fipName string) (*vpcv1.FloatingIP, error) {
	listFloatingIpsOptions := s.IBMVPCClients.VPCService.NewListFloatingIpsOptions()
	floatingIPs, _, err := s.IBMVPCClients.VPCService.ListFloatingIps(listFloatingIpsOptions)
	if err != nil {
		return nil, err
	}
	for _, fip := range floatingIPs.FloatingIps {
		if *fip.Name == fipName {
			return &fip, nil
		}
	}
	return nil, nil
}

// DeleteFloatingIP deletes a Floating IP associated with floating ip id
func (s *ClusterScope) DeleteFloatingIP() error {
	fipID := *s.IBMVPCCluster.Status.APIEndpoint.FIPID
	if fipID != "" {
		deleteFIPOption := &vpcv1.DeleteFloatingIPOptions{}
		deleteFIPOption.SetID(fipID)
		_, err := s.IBMVPCClients.VPCService.DeleteFloatingIP(deleteFIPOption)
		return err
	}
	return nil
}

// CreateSubnet creates a subnet within provided vpc and zone
func (s *ClusterScope) CreateSubnet() (*vpcv1.Subnet, error) {
	subnetName := s.IBMVPCCluster.Name + "-subnet"
	subnetReply, err := s.ensureSubnetUnique(subnetName)
	if err != nil {
		return nil, err
	} else if subnetReply != nil {
		//TODO need a reasonable wrapped error
		return subnetReply, nil
	}

	options := &vpcv1.CreateSubnetOptions{}
	cidrBlock, err := s.getSubnetAddrPrefix(s.IBMVPCCluster.Status.VPC.ID, s.IBMVPCCluster.Spec.Zone)
	if err != nil {
		return nil, err
	}
	subnetName = s.IBMVPCCluster.Name + "-subnet"
	options.SetSubnetPrototype(&vpcv1.SubnetPrototype{
		Ipv4CIDRBlock: &cidrBlock,
		Name:          &subnetName,
		VPC: &vpcv1.VPCIdentity{
			ID: &s.IBMVPCCluster.Status.VPC.ID,
		},
		Zone: &vpcv1.ZoneIdentity{
			Name: &s.IBMVPCCluster.Spec.Zone,
		},
	})
	subnet, _, err := s.IBMVPCClients.VPCService.CreateSubnet(options)
	if subnet != nil {
		pgw, err := s.createPublicGateWay(s.IBMVPCCluster.Status.VPC.ID, s.IBMVPCCluster.Spec.Zone)
		if err != nil {
			return subnet, err
		}
		if pgw != nil {
			if _, err := s.attachPublicGateWay(*subnet.ID, *pgw.ID); err != nil {
				return nil, err
			}
		}
	}
	return subnet, err
}

func (s *ClusterScope) getSubnetAddrPrefix(vpcID, zone string) (string, error) {
	options := &vpcv1.ListVPCAddressPrefixesOptions{
		VPCID: &vpcID,
	}
	addrCollection, _, err := s.IBMVPCClients.VPCService.ListVPCAddressPrefixes(options)

	if err != nil {
		return "", err
	}
	for _, addrPrefix := range addrCollection.AddressPrefixes {
		if *addrPrefix.Zone.Name == zone {
			return *addrPrefix.CIDR, nil
		}
	}
	return "", fmt.Errorf("not found a valid CIDR for VPC %s in zone %s", vpcID, zone)
}

func (s *ClusterScope) ensureSubnetUnique(subnetName string) (*vpcv1.Subnet, error) {
	options := &vpcv1.ListSubnetsOptions{}
	subnets, _, err := s.IBMVPCClients.VPCService.ListSubnets(options)

	if err != nil {
		return nil, err
	}
	for _, subnet := range subnets.Subnets {
		if *subnet.Name == subnetName {
			return &subnet, nil
		}
	}
	return nil, nil
}

// DeleteSubnet deletes a subnet associated with subnet id
func (s *ClusterScope) DeleteSubnet() error {
	subnetID := *s.IBMVPCCluster.Status.Subnet.ID

	// get the pgw id for given subnet, so we can delete it later
	getPGWOptions := &vpcv1.GetSubnetPublicGatewayOptions{}
	getPGWOptions.SetID(subnetID)
	pgw, _, err := s.IBMVPCClients.VPCService.GetSubnetPublicGateway(getPGWOptions)
	if pgw != nil && err == nil { // public gateway found
		// Unset the public gateway for subnet first
		err = s.detachPublicGateway(subnetID, *pgw.ID)
		if err != nil {
			return errors.Wrap(err, "Error when detaching publicgateway for subnet "+subnetID)
		}
	}

	// Delete subnet
	deleteSubnetOption := &vpcv1.DeleteSubnetOptions{}
	deleteSubnetOption.SetID(subnetID)
	_, err = s.IBMVPCClients.VPCService.DeleteSubnet(deleteSubnetOption)
	if err != nil {
		return errors.Wrap(err, "Error when deleting subnet ")
	}
	return err
}

func (s *ClusterScope) createPublicGateWay(vpcID string, zoneName string) (*vpcv1.PublicGateway, error) {
	options := &vpcv1.CreatePublicGatewayOptions{}
	options.SetVPC(&vpcv1.VPCIdentity{
		ID: &vpcID,
	})
	options.SetZone(&vpcv1.ZoneIdentity{
		Name: &zoneName,
	})
	publicGateway, _, err := s.IBMVPCClients.VPCService.CreatePublicGateway(options)
	return publicGateway, err
}

func (s *ClusterScope) attachPublicGateWay(subnetID string, pgwID string) (*vpcv1.PublicGateway, error) {
	options := &vpcv1.SetSubnetPublicGatewayOptions{}
	options.SetID(subnetID)
	options.SetPublicGatewayIdentity(&vpcv1.PublicGatewayIdentity{
		ID: &pgwID,
	})
	publicGateway, _, err := s.IBMVPCClients.VPCService.SetSubnetPublicGateway(options)
	return publicGateway, err
}

func (s *ClusterScope) detachPublicGateway(subnetID string, pgwID string) error {
	// Unset the publicgateway first, and then delete it
	unsetPGWOption := &vpcv1.UnsetSubnetPublicGatewayOptions{}
	unsetPGWOption.SetID(subnetID)
	_, err := s.IBMVPCClients.VPCService.UnsetSubnetPublicGateway(unsetPGWOption)
	if err != nil {
		return errors.Wrap(err, "Error when unsetting publicgateway for subnet "+subnetID)
	}

	// Delete the public gateway
	deletePGWOption := &vpcv1.DeletePublicGatewayOptions{}
	deletePGWOption.SetID(pgwID)
	_, err = s.IBMVPCClients.VPCService.DeletePublicGateway(deletePGWOption)
	if err != nil {
		return errors.Wrap(err, "Error when deleting publicgateway for subnet "+subnetID)
	}
	return err
}

// PatchObject persists the cluster configuration and status.
func (s *ClusterScope) PatchObject() error {
	return s.patchHelper.Patch(context.TODO(), s.IBMVPCCluster)
}

// Close closes the current scope persisting the cluster configuration and status.
func (s *ClusterScope) Close() error {
	return s.PatchObject()
}
