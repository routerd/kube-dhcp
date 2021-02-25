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
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	netv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

// DHCPServerReconciler reconciles a DHCPServer object
type DHCPServerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=networkattachmentdefinitions,verbs=get;list;watch;delete;update;create
// +kubebuilder:rbac:groups=dhcp.routerd.net,resources=dhcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=dhcp.routerd.net,resources=dhcpservers/status,verbs=get;update;patch

func (r *DHCPServerReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	_ = r.Log.WithValues("dhcpserver", req.NamespacedName)

	dhcpServer := &dhcpv1alpha1.DHCPServer{}
	if err = r.Get(ctx, req.NamespacedName, dhcpServer); err != nil {
		return res, client.IgnoreNotFound(err)
	}

	ippool, err := r.ippool(dhcpServer)
	if err != nil {
		return res, fmt.Errorf("preparing IPPool: %w", err)
	}
	dhcpServer.Status.IPPool = &dhcpv1alpha1.LocalObjectReference{
		Name: ippool.Name,
	}
	if err := reconcileIPPool(ctx, r.Client, ippool); err != nil {
		return res, fmt.Errorf("reconciling IPPool: %w", err)
	}

	// Reserve Gateway IP
	_, stop, err := r.ensureStaticGatewayLease(ctx, dhcpServer, ippool)
	if err != nil {
		return res, err
	} else if stop {
		return res, nil
	}

	// Reserve DHCP IP
	dhcpLease, stop, err := r.ensureDHCPLease(ctx, dhcpServer, ippool)
	if err != nil {
		return res, err
	} else if stop {
		return res, nil
	}
	dhcpServer.Status.Addresses = dhcpLease.Status.Addresses

	// NetworkAttachmentDefinition
	nad, stop, err := r.ensureNetworkAttachmentDefinition(ctx, dhcpServer, dhcpLease)
	if err != nil {
		return res, err
	} else if stop {
		return res, nil
	}

	deploy, stop, err := r.ensureDeployment(ctx, dhcpServer, nad)
	if err != nil {
		return res, err
	} else if stop {
		return res, nil
	}

	dhcpServer.Status.ObservedGeneration = deploy.Generation
	if deploy.Status.AvailableReplicas == deploy.Status.Replicas {
		dhcpServer.Status.Phase = "Ready"
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:               dhcpv1alpha1.Available,
			Status:             metav1.ConditionTrue,
			Reason:             "DeploymentReady",
			Message:            "DHCP Server Deployment ready",
			ObservedGeneration: deploy.Generation,
		})
	} else {
		dhcpServer.Status.Phase = "Unready"
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:               dhcpv1alpha1.Available,
			Status:             metav1.ConditionFalse,
			Reason:             "DeploymentUnready",
			Message:            "DHCP Server Deployment is not ready",
			ObservedGeneration: deploy.Generation,
		})
	}
	if err = r.Status().Update(ctx, dhcpServer); err != nil {
		return
	}
	return
}

func (r *DHCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dhcpv1alpha1.DHCPServer{}).
		Owns(&ipamv1alpha1.IPLease{}).
		Owns(&ipamv1alpha1.IPPool{}).
		Owns(&netv1.NetworkAttachmentDefinition{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// ensures that a static lease for the gateway IP exists,
// so we don't handout the router's IP address via IPAM.
func (r *DHCPServerReconciler) ensureStaticGatewayLease(
	ctx context.Context,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	ippool *ipamv1alpha1.IPPool,
) (_ *ipamv1alpha1.IPLease, stop bool, err error) {
	gatewayIPLease, err := staticGatewayIPLease(r.Scheme, dhcpServer, ippool)
	if err != nil {
		return nil, false, fmt.Errorf("preparing gateway IPLease: %w", err)
	}
	currentGatewayIPLease, err := reconcileIPLease(ctx, r.Client, gatewayIPLease)
	if err != nil {
		return nil, false, fmt.Errorf("reconciling gateway IPLease: %w", err)
	}
	if meta.IsStatusConditionFalse(
		currentGatewayIPLease.Status.Conditions,
		ipamv1alpha1.IPLeaseBound) {
		// Could not acquire static IPLease for gateway IP addresses
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:   dhcpv1alpha1.Available,
			Status: metav1.ConditionFalse,
			Reason: "UnboundGatewayIP",
			Message: fmt.Sprintf(
				"Could not lease Gateway IPs: %s",
				strings.Join(currentGatewayIPLease.Spec.Static.Addresses, ", ")),
			ObservedGeneration: dhcpServer.Generation,
		})
		dhcpServer.Status.ObservedGeneration = dhcpServer.Generation
		dhcpServer.Status.Phase = "Failed"
		return currentGatewayIPLease, true, r.Status().Update(ctx, dhcpServer)
	}
	if !meta.IsStatusConditionTrue(
		currentGatewayIPLease.Status.Conditions,
		ipamv1alpha1.IPLeaseBound) {
		// Static IPLease is not yet ready
		// Wait for next reconcile cycle.
		return currentGatewayIPLease, true, nil
	}

	// continue
	return currentGatewayIPLease, false, nil
}

// ensures the DHCP Server Pod has a Lease for it's own IP address.
func (r *DHCPServerReconciler) ensureDHCPLease(
	ctx context.Context,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	ippool *ipamv1alpha1.IPPool,
) (_ *ipamv1alpha1.IPLease, stop bool, err error) {
	dhcpIPLease, err := dhcpIPLease(r.Scheme, dhcpServer, ippool)
	if err != nil {
		return nil, false, fmt.Errorf("preparing gateway IPLease: %w", err)
	}
	currentDHCPIPLease, err := reconcileIPLease(ctx, r.Client, dhcpIPLease)
	if err != nil {
		return nil, false, fmt.Errorf("reconciling gateway IPLease: %w", err)
	}
	if meta.IsStatusConditionFalse(
		currentDHCPIPLease.Status.Conditions,
		ipamv1alpha1.IPLeaseBound) {
		// Could not acquire static IPLease for gateway IP addresses
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:               dhcpv1alpha1.Available,
			Status:             metav1.ConditionFalse,
			Reason:             "UnboundDHCPIP",
			Message:            "Could not lease IPs for the DHCP Server Pod.",
			ObservedGeneration: dhcpServer.Generation,
		})
		dhcpServer.Status.ObservedGeneration = dhcpServer.Generation
		dhcpServer.Status.Phase = "Failed"
		return currentDHCPIPLease, true, r.Status().Update(ctx, dhcpServer)
	}
	if !meta.IsStatusConditionTrue(
		currentDHCPIPLease.Status.Conditions,
		ipamv1alpha1.IPLeaseBound) {
		// Static IPLease is not yet ready
		// Wait for next reconcile cycle.
		return currentDHCPIPLease, true, nil
	}

	// continue
	return currentDHCPIPLease, false, nil
}

func (r *DHCPServerReconciler) ensureNetworkAttachmentDefinition(
	ctx context.Context,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	dhcpIPLease *ipamv1alpha1.IPLease,
) (_ *netv1.NetworkAttachmentDefinition, stop bool, err error) {
	nad, err := networkAttachmentDefinition(r.Scheme, dhcpServer, dhcpIPLease)
	if err != nil {
		return nil, false, fmt.Errorf("preparing NetworkAttachmentDefinition: %w", err)
	}
	if _, err := reconcileNAD(ctx, r.Client, nad); err != nil {
		return nil, false, fmt.Errorf("reconciling NetworkAttachmentDefinition: %w", err)
	}
	return nad, false, nil
}

func (r *DHCPServerReconciler) ensureDeployment(
	ctx context.Context,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	nad *netv1.NetworkAttachmentDefinition,
) (_ *appsv1.Deployment, stop bool, err error) {
	deploy, err := deployment(r.Scheme, dhcpServer, nad)
	if err != nil {
		return nil, false, fmt.Errorf("preparing Deployment: %w", err)
	}
	if _, err := reconcileDeployment(ctx, r.Client, deploy); err != nil {
		return nil, false, fmt.Errorf("reconciling Deployment: %w", err)
	}
	return deploy, false, nil
}

func (r *DHCPServerReconciler) ippool(
	dhcpServer *dhcpv1alpha1.DHCPServer,
) (*ipamv1alpha1.IPPool, error) {
	ippool := &ipamv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dhcpServer.Name,
			Namespace: dhcpServer.Namespace,
			Labels:    map[string]string{},
		},
		Spec: ipamv1alpha1.IPPoolSpec{
			LeaseDuration: &dhcpServer.Spec.LeaseDuration,
			IPv4:          &ipamv1alpha1.IPTypePool{},
		},
	}
	addCommonLabels(ippool.Labels, dhcpServer)

	if dhcpServer.Spec.IPv4 != nil {
		ippool.Spec.IPv4 = &ipamv1alpha1.IPTypePool{
			CIDR: dhcpServer.Spec.IPv4.Subnet,
		}
	}
	if dhcpServer.Spec.IPv6 != nil {
		ippool.Spec.IPv6 = &ipamv1alpha1.IPTypePool{
			CIDR: dhcpServer.Spec.IPv6.Subnet,
		}
	}

	if err := controllerutil.SetControllerReference(dhcpServer, ippool, r.Scheme); err != nil {
		return nil, err
	}
	return ippool, nil
}
