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
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"

	dhcpv1alpha1 "routerd.net/kube-dhcp/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

type dhcpResponse struct {
	IPv4 ipv4Response
	IPv6 ipv6Response
}

func (d *dhcpResponse) ApplyToV4(resp *dhcpv4.DHCPv4) {
	resp.Options.Update(
		dhcpv4.OptIPAddressLeaseTime(d.IPv4.LeaseDuration))
	resp.UpdateOption(dhcpv4.OptServerIdentifier(d.IPv4.ID))
	resp.Options.Update(dhcpv4.OptSubnetMask(d.IPv4.Mask))
	resp.YourIPAddr = d.IPv4.IP
	resp.Options.Update(dhcpv4.OptRouter(d.IPv4.Gateway))
}

type ipv4Response struct {
	ID            net.IP
	IP            net.IP
	Gateway       net.IP
	Mask          net.IPMask
	DNS           []net.IP
	LeaseDuration time.Duration
}

type ipv6Response struct {
	IPs           []net.IP
	Gateway       net.IP
	DNS           []net.IP
	LeaseDuration time.Duration
}

func dhcpResponseFromDHCPServerAndLease(
	dhcpServer *dhcpv1alpha1.DHCPServer,
	lease *ipamv1alpha1.IPLease,
) dhcpResponse {
	resp := dhcpResponse{}

	if dhcpServer.Spec.IPv4 != nil {
		_, ipv4net, _ := net.ParseCIDR(dhcpServer.Spec.IPv4.Subnet)
		resp.IPv4.Mask = ipv4net.Mask

		resp.IPv4.ID = onlyIPv4(stringSliceToIPSlice(
			dhcpServer.Status.Addresses))[0]
		resp.IPv4.Gateway = net.ParseIP(dhcpServer.Spec.IPv4.Gateway)
		resp.IPv4.DNS = stringSliceToIPSlice(
			dhcpServer.Spec.IPv4.NameServers)
		resp.IPv4.LeaseDuration = dhcpServer.Spec.LeaseDuration.Duration

		leaseIPs := onlyIPv4(stringSliceToIPSlice(lease.Status.Addresses))
		if len(leaseIPs) > 0 {
			resp.IPv4.IP = leaseIPs[0]
		}
	}

	if dhcpServer.Spec.IPv6 != nil {
		resp.IPv6.Gateway = net.ParseIP(dhcpServer.Spec.IPv6.Gateway)
		resp.IPv6.DNS = stringSliceToIPSlice(dhcpServer.Spec.IPv6.NameServers)
		resp.IPv6.LeaseDuration = dhcpServer.Spec.LeaseDuration.Duration

		resp.IPv6.IPs = onlyIPv6(
			stringSliceToIPSlice(lease.Status.Addresses))
	}

	return resp
}

func onlyIPv6(ips []net.IP) []net.IP {
	var out []net.IP
	for _, ip := range ips {
		if ip.To4() != nil {
			continue
		}

		out = append(out, ip)
	}
	return out
}

func onlyIPv4(ips []net.IP) []net.IP {
	var out []net.IP
	for _, ip := range ips {
		if ip.To4() == nil {
			continue
		}

		out = append(out, ip)
	}
	return out
}

func stringSliceToIPSlice(ipstrings []string) []net.IP {
	out := make([]net.IP, len(ipstrings))
	for _, ipstring := range ipstrings {
		out = append(out, net.ParseIP(ipstring))
	}
	return out
}
