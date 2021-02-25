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

package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/server6"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

type Config struct {
	Logger logr.Logger
	Client client.Client
	// Interface to bind the DHCP server onto.
	BindInterface string
	// Kubernetes Namespace we are running in.
	Namespace string
	// Name of the DHCPServer to query the configuration from.
	DHCPServerName string
	// IPv4 Address of the DHCP Server
	ServerIPv4 net.IP
}

func NewServer(c Config) *Server {
	return &Server{
		c: c,
	}
}

type Server struct {
	c Config
}

func (s *Server) dhcpv4Handler(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
	if req.OpCode != dhcpv4.OpcodeBootRequest {
		// only support BootRequests
		return
	}

	log := s.c.Logger.WithName("dhcpv4")
	resp, err := dhcpv4.NewReplyFromRequest(req)
	if err != nil {
		log.Error(err, "could not create reply for request")
		return
	}

	switch req.MessageType() {
	case dhcpv4.MessageTypeDiscover:
		resp.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeOffer))
	case dhcpv4.MessageTypeRequest:
		resp.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeAck))
	}

	ctx := context.Background()
	leaseCtx := leaseContext{}
	dhcpResp, err := s.response(ctx, req.ClientHWAddr, leaseCtx)
	if err != nil {
		log.Error(err, "handling request")
		return
	}

	dhcpResp.ApplyToV4(resp)
	if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
		log.Error(err, "writing response")
		return
	}
}

func (s *Server) handlerV6(req, resp dhcpv6.DHCPv6) error {
	return nil
}

func (s *Server) dhcpv6Handler(conn net.PacketConn, peer net.Addr, req dhcpv6.DHCPv6) {
	log := s.c.Logger.WithName("dhcpv6")

	msg, err := req.GetInnerMessage()
	if err != nil {
		log.Error(err, "could not get inner message from dhcpv6 request")
		return
	}

	var (
		resp dhcpv6.DHCPv6
	)
	switch msg.Type() {
	case dhcpv6.MessageTypeSolicit:
		if req.GetOneOption(dhcpv6.OptionRapidCommit) != nil {
			resp, err = dhcpv6.NewReplyFromMessage(msg)
		} else {
			resp, err = dhcpv6.NewAdvertiseFromSolicit(msg)
		}

	case dhcpv6.MessageTypeRequest, dhcpv6.MessageTypeConfirm,
		dhcpv6.MessageTypeRenew, dhcpv6.MessageTypeRebind,
		dhcpv6.MessageTypeRelease, dhcpv6.MessageTypeInformationRequest:
		resp, err = dhcpv6.NewReplyFromMessage(msg)

	default:
		log.Info("unhandled message type received", "type", msg.Type())
		return
	}
	if err != nil {
		log.Error(err, "creating reply from dhcpv6 message")
		return
	}

	if err := s.handlerV6(req, resp); err != nil {
		log.Error(err, "handling request")
		return
	}

	if _, err := conn.WriteTo(resp.ToBytes(), peer); err != nil {
		log.Error(err, "writing response")
		return
	}
}

func (s *Server) Start(ctx context.Context) error {
	ipv4Server, err := server4.NewServer(s.c.BindInterface, nil, s.dhcpv4Handler)
	if err != nil {
		return fmt.Errorf("creating DHVPv4 server: %w", err)
	}

	ipv6Server, err := server6.NewServer(s.c.BindInterface, nil, s.dhcpv6Handler)
	if err != nil {
		return fmt.Errorf("creating DHCPv6 server: %w", err)
	}

	serveDoneCh := make(chan error)
	defer close(serveDoneCh)

	go func() {
		serveDoneCh <- ipv4Server.Serve()
	}()
	go func() {
		serveDoneCh <- ipv6Server.Serve()
	}()

	select {
	case <-ctx.Done():
		ipv4Server.Close()
		ipv6Server.Close()

		<-serveDoneCh
		<-serveDoneCh

		return nil

	case err := <-serveDoneCh:
		return fmt.Errorf("serving: %w", err)
	}
}

type leaseContext struct {
	Hostname string
}

func (s *Server) response(
	ctx context.Context,
	mac net.HardwareAddr,
	lctx leaseContext,
) (dhcpResponse, error) {
	dhcpServer := &dhcpv1alpha1.DHCPServer{}
	if err := s.c.Client.Get(ctx, types.NamespacedName{
		Name:      s.c.DHCPServerName,
		Namespace: s.c.Namespace,
	}, dhcpServer); err != nil {
		return dhcpResponse{}, err
	}

	lease, err := s.ensureLease(ctx, dhcpServer, mac, lctx)
	if err != nil {
		return dhcpResponse{}, err
	}

	return dhcpResponseFromDHCPServerAndLease(dhcpServer, lease), nil
}

const (
	dhcpMACLabel  = "dhcp.routerd.net/mac"
	dhcpHostLabel = "dhcp.routerd.net/host"
)

func (s *Server) ensureLease(
	ctx context.Context,
	dhcpServer *dhcpv1alpha1.DHCPServer,
	mac net.HardwareAddr,
	lctx leaseContext,
) (*ipamv1alpha1.IPLease, error) {
	leaseName := base64.StdEncoding.
		EncodeToString(mac)
	lease := &ipamv1alpha1.IPLease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      leaseName,
			Namespace: s.c.Namespace,
			Labels: map[string]string{
				dhcpMACLabel: mac.String(),
			},
		},
		Spec: ipamv1alpha1.IPLeaseSpec{
			Pool: ipamv1alpha1.LocalObjectReference{
				Name: dhcpServer.Status.IPPool.Name,
			},
		},
	}
	if len(lctx.Hostname) > 0 {
		lease.Labels[dhcpHostLabel] = lctx.Hostname
	}

	existingLease := &ipamv1alpha1.IPLease{}
	err := s.c.Client.Get(ctx, types.NamespacedName{
		Name:      lease.Name,
		Namespace: lease.Namespace,
	}, existingLease)
	if errors.IsNotFound(err) {
		// create IPLease
		if err := s.c.Client.Create(ctx, lease); err != nil {
			return nil, err
		}
		if err := s.waitForLease(ctx, lease); err != nil {
			return nil, err
		}
		return lease, nil
	}
	if err != nil {
		return nil, err
	}

	// renew lease if exists
	existingLease.Spec.RenewTime = metav1.NowMicro()
	return existingLease, s.c.Client.Update(ctx, existingLease)
}

const (
	pollInterval = 500 * time.Millisecond
	leaseTimeout = 6 * time.Second
)

func (s *Server) waitForLease(
	ctx context.Context, lease *ipamv1alpha1.IPLease) error {
	ctx, cancel := context.WithTimeout(ctx, leaseTimeout)
	defer cancel()

	nn := types.NamespacedName{
		Name:      lease.Name,
		Namespace: lease.Namespace,
	}

	return wait.PollImmediateUntil(pollInterval, func() (bool, error) {
		existingLease := &ipamv1alpha1.IPLease{}
		err := s.c.Client.Get(ctx, nn, existingLease)
		if errors.IsNotFound(err) {
			// the object is gone...
			return false, err
		}
		if err != nil {
			// treat as a transient error and hope it will go away.
			return false, nil
		}

		if meta.IsStatusConditionTrue(
			existingLease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
			// yay!
			return true, nil
		}
		return false, nil
	}, ctx.Done())
}
