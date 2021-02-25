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

package controllers

import (
	"encoding/json"
	"fmt"
	"net"

	netv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilpointer "k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

// creates a static IPLease for the gateways of the DHCP server.
// this ensures that we don't handout the gateway IP via IPAM.
func staticGatewayIPLease(
	scheme *runtime.Scheme,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	ippool *ipamv1alpha1.IPPool,
) (*ipamv1alpha1.IPLease, error) {
	var addresses []string
	if dhcpServer.Spec.IPv4 != nil {
		addresses = append(addresses, dhcpServer.Spec.IPv4.Gateway)
	}
	if dhcpServer.Spec.IPv6 != nil {
		addresses = append(addresses, dhcpServer.Spec.IPv6.Gateway)
	}

	iplease := &ipamv1alpha1.IPLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ippool.Name + "-gateway",
			Namespace: ippool.Namespace,
			Labels:    map[string]string{},
		},
		Spec: ipamv1alpha1.IPLeaseSpec{
			Pool: ipamv1alpha1.LocalObjectReference{
				Name: ippool.Name,
			},
			Static: &ipamv1alpha1.StaticIPLease{
				Addresses: addresses,
			},
		},
	}

	addCommonLabels(iplease.Labels, dhcpServer)
	if err := controllerutil.SetControllerReference(dhcpServer, iplease, scheme); err != nil {
		return nil, err
	}
	return iplease, nil
}

func dhcpIPLease(
	scheme *runtime.Scheme,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	ippool *ipamv1alpha1.IPPool,
) (*ipamv1alpha1.IPLease, error) {
	iplease := &ipamv1alpha1.IPLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ippool.Name + "-dhcp",
			Namespace: ippool.Namespace,
			Labels:    map[string]string{},
		},
		Spec: ipamv1alpha1.IPLeaseSpec{
			Pool: ipamv1alpha1.LocalObjectReference{
				Name: ippool.Name,
			},
		},
	}

	addCommonLabels(iplease.Labels, dhcpServer)
	if err := controllerutil.SetControllerReference(dhcpServer, iplease, scheme); err != nil {
		return nil, err
	}
	return iplease, nil
}

func networkAttachmentDefinition(
	scheme *runtime.Scheme,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	dhcpIPLease *ipamv1alpha1.IPLease,
) (*netv1.NetworkAttachmentDefinition, error) {
	if dhcpServer.Spec.NetworkAttachment.Type != dhcpv1alpha1.Bridge {
		return nil, fmt.Errorf(
			"unsupported network attachment type: %s",
			dhcpServer.Spec.NetworkAttachment.Type)
	}
	if !meta.IsStatusConditionTrue(
		dhcpIPLease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
		return nil, fmt.Errorf("DHCP IPLease is not bound")
	}

	var (
		ipv4Mask net.IPMask
		ipv6Mask net.IPMask
	)
	if dhcpServer.Spec.IPv4 != nil {
		_, ipv4Net, _ := net.ParseCIDR(dhcpServer.Spec.IPv4.Subnet)
		ipv4Mask = ipv4Net.Mask
	}
	if dhcpServer.Spec.IPv6 != nil {
		_, ipv6Net, _ := net.ParseCIDR(dhcpServer.Spec.IPv6.Subnet)
		ipv6Mask = ipv6Net.Mask
	}

	var addresses []map[string]string
	for _, addr := range dhcpIPLease.Status.Addresses {
		ip := net.ParseIP(addr)
		if ip.To4() != nil {
			// ipv4
			addresses = append(addresses, map[string]string{
				"address": (&net.IPNet{IP: ip, Mask: ipv4Mask}).String()})
		} else {
			addresses = append(addresses, map[string]string{
				"address": (&net.IPNet{IP: ip, Mask: ipv6Mask}).String()})
		}
	}
	nadConfigJSON := map[string]interface{}{
		"cniVersion": "0.3.1",
		"name":       dhcpServer.Namespace + "-" + dhcpServer.Name,
		"plugins": []map[string]interface{}{
			{
				"type":   "bridge",
				"bridge": dhcpServer.Spec.NetworkAttachment.Bridge.Name,
				"ipam": map[string]interface{}{
					"type":      "static",
					"addresses": addresses,
				},
			},
		},
	}
	nadConfigBytes, err := json.MarshalIndent(nadConfigJSON, "", "  ")
	if err != nil {
		return nil, err
	}

	nad := &netv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name,
			Namespace: dhcpServer.Namespace,
			Labels:    map[string]string{},
		},
		Spec: netv1.NetworkAttachmentDefinitionSpec{
			Config: string(nadConfigBytes),
		},
	}
	addCommonLabels(nad.Labels, dhcpServer)
	if err := controllerutil.SetControllerReference(dhcpServer, nad, scheme); err != nil {
		return nil, err
	}
	return nad, nil
}

const networksAnnotations = "k8s.v1.cni.cncf.io/networks"

func deployment(
	scheme *runtime.Scheme,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	nad *netv1.NetworkAttachmentDefinition,
) (*appsv1.Deployment, error) {
	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name,
			Namespace: dhcpServer.Namespace,
			Labels:    map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: utilpointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
			Strategy: appsv1.DeploymentStrategy{
				// We only want a single instance running at any given time.
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Annotations: map[string]string{
						networksAnnotations: nad.Name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							ImagePullPolicy: corev1.PullAlways,
							Name:            "dhcp-server",
							Image:           "quay.io/routerd/kube-dhcp:implementation-8027e69",
							Env: []corev1.EnvVar{
								{Name: "DHCP_BIND_INTERFACE", Value: "net1"},
								{Name: "DHCP_SERVER_NAME", Value: dhcpServer.Name},
								{
									Name: "KUBERNETES_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	addCommonLabels(deploy.Labels, dhcpServer)
	addCommonLabels(deploy.Spec.Selector.MatchLabels, dhcpServer)
	addCommonLabels(deploy.Spec.Template.ObjectMeta.Labels, dhcpServer)

	if err := controllerutil.SetControllerReference(dhcpServer, deploy, scheme); err != nil {
		return nil, err
	}

	return deploy, nil
}

const (
	commonNameLabel      = "app.kubernetes.io/name"
	commonComponentLabel = "app.kubernetes.io/component"
	commonInstanceLabel  = "app.kubernetes.io/instance"
	commonManagedByLabel = "app.kubernetes.io/managed-by"
)

func addCommonLabels(labels map[string]string, dhcpServer *dhcpv1alpha1.DHCPServer) {
	if labels == nil {
		return
	}

	labels[commonNameLabel] = "kube-dhcp"
	labels[commonComponentLabel] = "dhcp-server"
	labels[commonManagedByLabel] = "kube-dhcp-operator"
	labels[commonInstanceLabel] = dhcpServer.Name
}
