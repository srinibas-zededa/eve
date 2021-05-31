// Copyright (c) 2018 Zededa, Inc.
// SPDX-License-Identifier: Apache-2.0

// Network Instance underlay network Application Number Management

package zedrouter

import (
	"errors"
	"fmt"
	"net"

	"github.com/lf-edge/eve/pkg/pillar/types"
)

// Network Instance Application number/IP Address Management
// -----------------------------------
// Consists of following data information
//  - (Mac Address -> IP Address) Mapping
//  - (IP Address  -> Mac Address) Mapping
//  - AppNumBitMap, bitmap for DhcpRange, limited to 254(max)
//  - Address Map, in persist storage used
//        across reboot and deactivate/activate cycle
// A given IP Address can be directly mapped to an App Number
// ipAddr -> DhcpRange.Start + AppNum                   (1)
// ----------------------------------
// Dhcp range can be at any place, inside the subnet range, and does
// not include the network address and broadcast address
// When dhcp range is not configured, the second half of the subnet
// range is assigned as the dhcp range.
// By default, bridge/gateway IP address is first Host IP Address, in the
// Dhcp range, unless explicitly configured..
// The DhcpRange.Start starts at the next Host IP Address
// and DhcpRange.End defines the end Host IP Address, for a subnet.
// e.g., IP Subnet (10.1.0.0/24)
// Subnet Range (10.1.0.1 - 10.1.0.254)
// Bridge IP Address/Gateway (10.1.0.1)
// DhcpRange (10.1.0.2 - 10.1.0.254)
// User can also have a specific Dhcp Address Range,
// e.g., Dhcp Range (10.1.0.2 - 10.1.0.10)  -- 9 Host IP Addresses
// Static IP Address is a user defined IP Address for an
// App Instance, it can pan across the Subnet, irrespective
// of a given Dhcp Range
// Similarly, the Bridge IP Address can also pan across the subnet.
//   [-------------------------.Subnet Range -------------------------------]
//    First IP
//       1     [----------254(max)-------------------------]
//   [Gateway] [DhcpRange.Start ------------- DhcpRange.End]
//             [------Dynamic Dhcp Assigned IP-------------]
//             [-------------------Static IP Address------------------------]
// Irrespective of the app instance activation sequence, if a
// given IP Address is assigned once, should not change across
// reboot. This is managed through Addr Map in persistent storage(Restore on
// Boot up).
//  IPAddr Module (IP->Mac and Mac->IP Mapping)
//  -------------
//    - loookupOrAllocateIPv4()
//    - releaseIPv4FromNetworkInstance()
//    - networkInstanceIPAddrSet()
//    - networkInstanceIPAddrClear()
//    - networkInstanceIsDuplicateIP()

// set IP Address mapping information
func networkInstanceIPAddrSet(ctx *zedrouterContext,
	netInstStatus *types.NetworkInstanceStatus, networkID string,
	appID string, appNum int, ipAddr net.IP, mac net.HardwareAddr) {
	recordIPAssignment(ctx, netInstStatus, ipAddr, mac)
	bitMapAppNumAdd(ctx, netInstStatus, appNum)
	persistentAddrMapAdd(ctx, networkID, appID, appNum)
}

// clear IP Address mapping information
func networkInstanceIPAddrClear(ctx *zedrouterContext,
	netInstStatus *types.NetworkInstanceStatus, networkID string,
	appID string, appNum int, mac net.HardwareAddr) {
	uNetAppNumDelete(ctx, netInstStatus, appID, appNum)
	persistentAddrMapDelete(ctx, networkID, appID)
	releaseIPv4FromNetworkInstance(ctx, netInstStatus, mac)
}

// check for duplicate assignment
func networkInstanceIsDuplicateIP(status *types.NetworkInstanceStatus,
	appNum int, mac net.HardwareAddr) bool {
	ipAddr := types.AddToIP(status.DhcpRange.Start, appNum)
	if macStr, ok := status.MacAssignments[ipAddr.String()]; ok {
		if macStr != mac.String() {
			return true
		}
	}
	return false
}

// recordIPAssigment updates status and publishes the result
func recordIPAssignment(ctx *zedrouterContext,
	status *types.NetworkInstanceStatus, ip net.IP, mac net.HardwareAddr) {
	if status != nil {
		status.IPAssignments[mac.String()] = ip
		status.MacAssignments[ip.String()] = mac.String()
		// Publish the allocation
		publishNetworkInstanceStatus(ctx, status)
	}
}

// Returns an IP address as a string, or "" on error
func lookupOrAllocateIPv4(ctx *zedrouterContext,
	netInstStatus *types.NetworkInstanceStatus, appID string, appNum int,
	mac net.HardwareAddr) (string, error) {
	log.Functionf("lookupOrAllocateIPv4(%s-%s): mac:%s\n",
		netInstStatus.DisplayName, netInstStatus.Key(), mac.String())
	if appNum == types.AppNumInvalid {
		log.Functionf("(%s)no ip address", mac.String())
		return "", nil
	}
	// Lookup to see if it exists
	if ip, ok := netInstStatus.IPAssignments[mac.String()]; ok {
		log.Functionf("found Ip addr ( %s) for mac(%s)\n",
			ip.String(), mac.String())
		return ip.String(), nil
	}

	log.Functionf("bridgeName %s Subnet %v range %v-%v\n",
		netInstStatus.BridgeName, netInstStatus.Subnet,
		netInstStatus.DhcpRange.Start.String(),
		netInstStatus.DhcpRange.End.String())

	if netInstStatus.DhcpRange.Start == nil {
		if netInstStatus.Type == types.NetworkInstanceTypeSwitch {
			log.Functionf("%s-%s switch means no bridgeIpAddr",
				netInstStatus.DisplayName, netInstStatus.Key())
			return "", nil
		}
		log.Fatalf("%s-%s: nil DhcpRange.Start",
			netInstStatus.DisplayName, netInstStatus.Key())
	}

	// get ip address
	a := types.AddToIP(netInstStatus.DhcpRange.Start, appNum)

	// assigned IP Address must be inside subnet address range
	if !netInstStatus.Subnet.Contains(a) {
		errStr := fmt.Sprintf("lookupOrAllocateIPv4(%s), out of range(%s)",
			mac.String(), a.String())
		return "", errors.New(errStr)
	}
	networkID := netInstStatus.UUID.String()
	networkInstanceIPAddrSet(ctx, netInstStatus, networkID,
		appID, appNum, a, mac)
	log.Functionf("lookupOrAllocateIPv4(%s) found %s\n",
		mac.String(), a.String())
	return a.String(), nil
}

// releaseIPv4
//	XXX TODO - This should be a method in NetworkInstanceSm
func releaseIPv4FromNetworkInstance(ctx *zedrouterContext,
	status *types.NetworkInstanceStatus,
	mac net.HardwareAddr) error {

	if status != nil {
		log.Functionf("releaseIPv4(%s)\n", mac.String())
		// Lookup to see if it exists
		ip, ok := status.IPAssignments[mac.String()]
		if !ok {
			errStr := fmt.Sprintf("releaseIPv4: not found %s for %s",
				mac.String(), status.Key())
			log.Error(errStr)
			return errors.New(errStr)
		}
		delete(status.IPAssignments, mac.String())
		delete(status.MacAssignments, ip.String())
		publishNetworkInstanceStatus(ctx, status)
	}
	return nil
}
