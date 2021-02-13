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
	netv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DHCPServerSpec defines the desired state of DHCPServer.
type DHCPServerSpec struct {
	// DHCP IPv4 specific settings.
	IPv4 *DHCPServerIPv4 `json:"ipv4,omitempty"`
	// DHCP IPv6 specific settings.
	IPv6 *DHCPServerIPv6 `json:"ipv6,omitempty"`
	// NetworkAttachmentDefinition to create for this DHCP Server deployment.
	NetworkAttachmentDefinitionTemplate NetworkAttachmentDefinitionTemplate `json:"networkAttachmentDefinitionTemplate"`
}

// DHCP Server Settings for IPv4.
type DHCPServerIPv4 struct {
	// Subnet CIDR this DHCP Server should operate on.
	Subnet string `json:"subnet"`
	// Gateway IPv4 address of the router for this subnet.
	Gateway string `json:"gateway"`
	// NameServers to point clients to.
	NameServers []string `json:"nameServers,omitempty"`
	// Range for automatic address assignment.
	Range *IPRange `json:"range,omitempty"`
	// Lease timeout duration.
	Lease metav1.Duration `json:"lease"`
}

// DHCP Server Settings for IPv6.
type DHCPServerIPv6 struct{}

// IP Range from to.
type IPRange struct {
	// Start IP - first IP of the range (included)
	Start string `json:"start"`
	// Stop IP - last IP of the range (included)
	Stop string `json:"stop"`
}

type NetworkAttachmentDefinitionTemplate struct {
	// May contain labels and annotations that will be copied into the PVC
	// when creating it. No other fields are allowed and will be rejected during
	// validation.
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The specification for the NetworkAttachmentDefinition.
	// The entire content is copied into the NAD that gets created from this template.
	// .spec.config.name will be set to the NetworkAttachmentDefinition name.
	Spec netv1.NetworkAttachmentDefinitionSpec `json:"spec"`
}

// DHCPServerStatus defines the observed state of DHCPServer
type DHCPServerStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is a list of status conditions ths object is in.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Human readable status aggregated from conditions.
	Phase string `json:"phase,omitempty"`
}

// DHCPServer is the Schema for the dhcpservers API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
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
