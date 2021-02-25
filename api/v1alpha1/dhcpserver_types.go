/*
Copyright 2021 The routerd authors.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DHCPServerSpec defines the desired state of DHCPServer.
type DHCPServerSpec struct {
	// Lease timeout duration.
	LeaseDuration metav1.Duration `json:"leaseDuration"`

	// DHCP IPv4 specific settings.
	IPv4 *DHCPServerIPv4 `json:"ipv4,omitempty"`
	// DHCP IPv6 specific settings.
	IPv6 *DHCPServerIPv6 `json:"ipv6,omitempty"`

	// Configures how the DHCP Server is attached to a network.
	NetworkAttachment NetworkAttachment `json:"networkAttachment"`
}

// DHCP Server Settings for IPv4.
type DHCPServerIPv4 struct {
	// Subnet CIDR this DHCP Server should operate on.
	Subnet string `json:"subnet"`
	// Gateway IPv4 address of the router for this subnet.
	Gateway string `json:"gateway"`
	// NameServers to point clients to.
	NameServers []string `json:"nameServers,omitempty"`
}

// DHCP Server Settings for IPv6.
type DHCPServerIPv6 struct {
	// Subnet CIDR this DHCP Server should operate on.
	Subnet string `json:"subnet"`
	// Gateway IPv4 address of the router for this subnet.
	Gateway string `json:"gateway"`
	// NameServers to point clients to.
	NameServers []string `json:"nameServers,omitempty"`
}

type NetworkAttachmentType string

const (
	Bridge NetworkAttachmentType = "Bridge"
)

type NetworkAttachment struct {
	// Type of network attachment for the DHCP server.
	// Supported Types: "Bridge"
	//
	// Bridge uses multus and creates a NetworkAttachmentDefinition
	// pointing to the given linux bridge.
	// +kubebuilder:default="Bridge"
	// +kubebuilder:validation:Enum=Bridge
	Type NetworkAttachmentType `json:"type"`
	// Bridge attachment configuration.
	Bridge *NetworkAttachmentBridge `json:"bridge"`
}

// NetworkAttachment settings for type="Bridge"
type NetworkAttachmentBridge struct {
	// Name of the bridge device to bind to.
	Name string `json:"name"`
}

const (
	// Available condition indicates whether the DHCP Server is alive and well.
	Available = "Available"
)

// DHCPServerStatus defines the observed state of DHCPServer
type DHCPServerStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is a list of status conditions ths object is in.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Human readable status aggregated from conditions.
	Phase string `json:"phase,omitempty"`
	// References the IPPool object for this DHCP server.
	IPPool *LocalObjectReference `json:"ippool,omitempty"`
	// Addresses the DHCP server is available at.
	Addresses []string `json:"addresses,omitempty"`
}

type LocalObjectReference struct {
	Name string `json:"name"`
}

// DHCPServer is the Schema for the dhcpservers API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Addresses",type="string",JSONPath=".status.addresses"
// +kubebuilder:printcolumn:name="Pool",type="string",JSONPath=".status.ippool.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type DHCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DHCPServerSpec   `json:"spec,omitempty"`
	Status DHCPServerStatus `json:"status,omitempty"`
}

// DHCPServerList contains a list of DHCPServer
// +kubebuilder:object:root=true
type DHCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DHCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DHCPServer{}, &DHCPServerList{})
}
