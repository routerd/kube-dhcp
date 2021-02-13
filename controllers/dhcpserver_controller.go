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
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	netv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilpointer "k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
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
	if err = r.Get(ctx, req.NamespacedName, dhcpServer); client.IgnoreNotFound(err) != nil {
		return
	}

	nad, err := r.networkAttachmentDefinition(dhcpServer)
	if err != nil {
		err = fmt.Errorf("could not prepare NetworkAttachmentDefinition: %w", err)
		return
	}

	deploy, err := r.deployment(dhcpServer, nad)
	if err != nil {
		err = fmt.Errorf("could not prepare Deployment: %w", err)
		return
	}

	if err = r.reconcileNAD(ctx, nad); err != nil {
		return
	}
	currentDeploy, err := r.reconcileDeployment(ctx, deploy)
	if err != nil {
		return
	}

	dhcpServer.Status.ObservedGeneration = currentDeploy.Generation
	if currentDeploy.Status.AvailableReplicas == currentDeploy.Status.Replicas {
		dhcpServer.Status.Phase = "Ready"
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "DeploymentReady",
			Message:            "DHCP Server Deployment ready",
			ObservedGeneration: currentDeploy.Generation,
		})
	} else {
		dhcpServer.Status.Phase = "Unready"
		meta.SetStatusCondition(&dhcpServer.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "DeploymentUnready",
			Message:            "DHCP Server Deployment is not ready",
			ObservedGeneration: currentDeploy.Generation,
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
		Complete(r)
}

func (r *DHCPServerReconciler) reconcileNAD(
	ctx context.Context, nad *netv1.NetworkAttachmentDefinition) error {
	currentNAD := &netv1.NetworkAttachmentDefinition{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      nad.Name,
		Namespace: nad.Namespace,
	}, currentNAD)
	if errors.IsNotFound(err) {
		return r.Create(ctx, nad)
	}
	if err != nil {
		return err
	}

	if equality.Semantic.DeepDerivative(nad.Spec, currentNAD.Spec) {
		// objects are equal
		return nil
	}
	// update
	currentNAD.Spec = nad.Spec
	return r.Update(ctx, currentNAD)
}

func (r *DHCPServerReconciler) reconcileDeployment(
	ctx context.Context, deploy *appsv1.Deployment,
) (currentDeploy *appsv1.Deployment, err error) {
	currentDeploy = &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      deploy.Name,
		Namespace: deploy.Namespace,
	}, currentDeploy)
	if errors.IsNotFound(err) {
		return deploy, r.Create(ctx, deploy)
	}
	if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepDerivative(deploy.Spec, currentDeploy.Spec) {
		// objects are equal
		return currentDeploy, nil
	}
	// update
	currentDeploy.Spec = deploy.Spec
	return currentDeploy, r.Update(ctx, currentDeploy)
}

const (
	commonNameLabel      = "app.kubernetes.io/name"
	commonComponentLabel = "app.kubernetes.io/component"
	commonInstanceLabel  = "app.kubernetes.io/instance"
	commonManagedByLabel = "app.kubernetes.io/managed-by"

	networksAnnotations = "k8s.v1.cni.cncf.io/networks"
)

func (r *DHCPServerReconciler) deployment(
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
							Name:  "dhcp-server",
							Image: "nginx",
						},
					},
				},
			},
		},
	}
	addCommonLabels(deploy.Labels, dhcpServer)
	addCommonLabels(deploy.Spec.Selector.MatchLabels, dhcpServer)
	addCommonLabels(deploy.Spec.Template.ObjectMeta.Labels, dhcpServer)

	if err := controllerutil.SetControllerReference(dhcpServer, deploy, r.Scheme); err != nil {
		return nil, err
	}

	return deploy, nil
}

func (r *DHCPServerReconciler) networkAttachmentDefinition(
	dhcpServer *dhcpv1alpha1.DHCPServer) (*netv1.NetworkAttachmentDefinition, error) {
	labels := dhcpServer.Spec.NetworkAttachmentDefinitionTemplate.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	addCommonLabels(labels, dhcpServer)

	nad := &netv1.NetworkAttachmentDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name:        dhcpServer.Name,
			Namespace:   dhcpServer.Namespace,
			Labels:      labels,
			Annotations: dhcpServer.Spec.NetworkAttachmentDefinitionTemplate.Annotations,
		},
		Spec: dhcpServer.Spec.NetworkAttachmentDefinitionTemplate.Spec,
	}
	// set name
	var cniConfig map[string]interface{}
	if err := json.Unmarshal([]byte(nad.Spec.Config), &cniConfig); err != nil {
		// TODO: non transisten error
		return nil, err
	}
	// must be unique to cluster
	cniConfig["name"] = nad.Namespace + "-" + nad.Name
	cniConfigJson, err := json.Marshal(cniConfig)
	if err != nil {
		return nil, err
	}
	nad.Spec.Config = string(cniConfigJson)

	if err := controllerutil.SetControllerReference(dhcpServer, nad, r.Scheme); err != nil {
		return nil, err
	}

	return nad, nil
}

func addCommonLabels(labels map[string]string, dhcpServer *dhcpv1alpha1.DHCPServer) {
	if labels == nil {
		return
	}

	labels[commonNameLabel] = "kube-dhcp"
	labels[commonComponentLabel] = "dhcp-server"
	labels[commonManagedByLabel] = "kube-dhcp-operator"
	labels[commonInstanceLabel] = dhcpServer.Name
}
