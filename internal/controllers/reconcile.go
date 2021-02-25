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

	netv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

// reconciles a NetworkAttachmentDefinition and returns the current object as observed.
func reconcileNAD(
	ctx context.Context,
	c client.Client, nad *netv1.NetworkAttachmentDefinition,
) (*netv1.NetworkAttachmentDefinition, error) {
	currentNAD := &netv1.NetworkAttachmentDefinition{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      nad.Name,
		Namespace: nad.Namespace,
	}, currentNAD)
	if errors.IsNotFound(err) {
		return nad, c.Create(ctx, nad)
	}
	if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepDerivative(nad.Spec, currentNAD.Spec) {
		// objects are equal
		return currentNAD, nil
	}
	// update
	currentNAD.Spec = nad.Spec
	return currentNAD, c.Update(ctx, currentNAD)
}

// reconciles a Deployment and returns the current object as observed.
func reconcileDeployment(
	ctx context.Context,
	c client.Client, deploy *appsv1.Deployment,
) (currentDeploy *appsv1.Deployment, err error) {
	currentDeploy = &appsv1.Deployment{}
	err = c.Get(ctx, types.NamespacedName{
		Name:      deploy.Name,
		Namespace: deploy.Namespace,
	}, currentDeploy)
	if errors.IsNotFound(err) {
		return deploy, c.Create(ctx, deploy)
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
	return currentDeploy, c.Update(ctx, currentDeploy)
}

// reconciles an IPPool and returns the current object as observed.
func reconcileIPPool(
	ctx context.Context,
	c client.Client, ippool *ipamv1alpha1.IPPool) error {
	currentIPPool := &ipamv1alpha1.IPPool{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      ippool.Name,
		Namespace: ippool.Namespace,
	}, currentIPPool)
	if errors.IsNotFound(err) {
		return c.Create(ctx, ippool)
	}
	if err != nil {
		return err
	}

	if equality.Semantic.DeepDerivative(ippool.Spec, currentIPPool.Spec) {
		// objects are equal
		return nil
	}
	// update
	currentIPPool.Spec = ippool.Spec
	return c.Update(ctx, currentIPPool)
}

// reconciles an IPLease and returns the current object as observed.
func reconcileIPLease(
	ctx context.Context,
	c client.Client, iplease *ipamv1alpha1.IPLease,
) (*ipamv1alpha1.IPLease, error) {
	// Reconcile the object.
	currentIPLease := &ipamv1alpha1.IPLease{}
	err := c.Get(ctx, types.NamespacedName{
		Name:      iplease.Name,
		Namespace: iplease.Namespace,
	}, currentIPLease)
	if errors.IsNotFound(err) {
		return iplease, c.Create(ctx, iplease)
	}
	if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepDerivative(iplease.Spec, currentIPLease.Spec) {
		// objects are equal
		return currentIPLease, nil
	}
	// update
	currentIPLease.Spec = iplease.Spec
	return currentIPLease, c.Update(ctx, currentIPLease)
}
