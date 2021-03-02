/*
Copyright 2021.

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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// FabricNetworkSpec defines the desired state of FabricNetwork
type FabricNetworkSpec struct {
	Configtx Configtx `json:"configtx,omitempty"`
	Genesis  Genesis  `json:"genesis,omitempty"`

	// Adds additional DNS entries to /etc/hosts files of pods
	// This is provided for communication with external peers/orderers
	// if useActualDomains is true, Fabric Operator will still create internal hostAliases and append to this one
	HostAliases []corev1.HostAlias `json:"hostAliases,omitempty"`

	Topology Topology `json:"topology,omitempty"`
	Network  Network  `json:"network,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	HlfKube runtime.RawExtension `json:"hlf-kube,omitempty"`
}

// FabricNetworkStatus defines the observed state of FabricNetwork
type FabricNetworkStatus struct {
	State    State  `json:"state,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	Workflow string `json:"workflow,omitempty"`
}

type State string

const (
	StateNew                  State = "New"
	StateHelmChartInstalled   State = "HelmChartInstalled"
	StateHelmChartNeedsUpdate State = "HelmChartNeedsUpdate"
	StateHelmChartReady       State = "HelmChartReady"
	StateChannelFlowSubmitted State = "ChannelFlowSubmitted"
	StateChannelFlowCompleted State = "ChannelFlowCompleted"
	StateRejected             State = "Rejected"
	StateInvalid              State = "Invalid"
	StateFailed               State = "Failed"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=fabricnetworks,shortName=fn

// FabricNetwork is the Schema for the fabricnetworks API
type FabricNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FabricNetworkSpec   `json:"spec,omitempty"`
	Status FabricNetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// FabricNetworkList contains a list of FabricNetwork
type FabricNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FabricNetwork `json:"items"`
}

// Source of the configtx.yaml file. either a Kubernetes Secret or a file.
// file can only be used via CLI
type Configtx struct {
	File   string `json:"file,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// Source of the genesis block. either a Kubernetes Secret or a file.
// If none provided Fabric Operator will create the genesis block.
// file can only be used via CLI
type Genesis struct {
	File   string `json:"file,omitempty"`
	Secret string `json:"secret,omitempty"`
}

// Topology of the Fabric network managed by Fabric Operator.
// Also contains some top level properties which is applied to whole network.
type Topology struct {

	// Hyperledger Fabric Version
	Version string `json:"version,omitempty"`
	// TLS enabled?
	TlsEnabled bool `json:"tlsEnabled,omitempty"`
	// use actual domain names like peer0.atlantis.com instead of internal service names
	UseActualDomains bool `json:"useActualDomains,omitempty"`

	// Orderer organizations
	OrdererOrg []OrdererOrg `json:"ordererOrgs"`
	// Peer organizations
	PeerOrg []PeerOrg `json:"peerOrgs"`
}

// Orderer organization
type OrdererOrg struct {
	// Name of organization
	Name string `json:"name,omitempty"`
	// Domain of organization
	Domain string `json:"domain,omitempty"`
	// orderer hosts list, at least one is required
	Hosts []string `json:"hosts,omitempty"`
}

// Peer organization
type PeerOrg struct {
	// Name of organization
	Name string `json:"name,omitempty"`
	// Domain of organization
	Domain string `json:"domain,omitempty"`
	// number of peers
	PeerCount int32 `json:"peerCount,omitempty"`
}

type Network struct {
	GenesisProfile  string `json:"genesisProfile,omitempty"`
	SystemChannelID string `json:"systemChannelID,omitempty"`

	Channel   []Channel   `json:"channels"`
	Chaincode []Chaincode `json:"chaincodes"`
}

type Channel struct {
	// Name of channel
	Name string `json:"name,omitempty"`
	// Peer organizations in the channel
	Orgs []string `json:"orgs,omitempty"`
}

type Chaincode struct {
	// Name of chaincode
	Name string `json:"name,omitempty"`
	// Version of chaincode. If defined, this will override the global chaincode.version value
	Version string `json:"version,omitempty"`
	// Programming language of chaincode. If defined, this will override the global chaincode.language value
	Language string `json:"language,omitempty"`
	// Chaincode will be installed to all peers in these peer organizations
	Orgs []string `json:"orgs,omitempty"`
	// Channels are we instantiating/upgrading this chaincode
	CcChannel []CcChannel `json:"channels"`
}

// Chaincode channel
type CcChannel struct {
	// Name of channel
	Name string `json:"name,omitempty"`
	// Chaincode will be instantiated/upgraded using the first peer in the first organization.
	// Chaincode will be invoked on all peers in these organizations.
	Orgs []string `json:"orgs,omitempty"`
	// Chaincode policy
	Policy string `json:"policy,omitempty"`
}

func init() {
	SchemeBuilder.Register(&FabricNetwork{}, &FabricNetworkList{})
}
