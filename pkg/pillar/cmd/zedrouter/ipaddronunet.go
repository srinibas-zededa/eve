// Copyright (c) 2021 Zededa, Inc.
// SPDX-License-Identifier: Apache-2.0

// Network Instance underlay network Application Number Management

package zedrouter

import (
	"fmt"
	"github.com/lf-edge/eve/pkg/pillar/types"
)

func appNumsOnUNetAllocate(ctx *zedrouterContext,
	config *types.AppNetworkConfig) {
	for _, ulConfig := range config.UnderlayNetworkList {
		appID := config.UUIDandVersion.UUID
		networkID := ulConfig.Network
		netStatus := lookupNetworkInstanceStatus(ctx, networkID.String())
		if netStatus != nil {
			baseSize := netStatus.DhcpRange.Size
			appNum, err := appNumOnUNetAllocate(ctx, networkID, appID,
				baseSize, false)
			if appNum == types.AppNumInvalid || err != nil {
				errStr := fmt.Sprintf("App Num get fail :%s", err)
				log.Errorf("appNumsOnAppNetworkAllocate(%s, %s): fail: %s\n",
					networkID.String(), appID.String(), errStr)
			}
		}
	}
}

func appNumsOnUNetFree(ctx *zedrouterContext,
	status *types.AppNetworkStatus) {
	appID := status.UUIDandVersion.UUID
	for ulNum := 0; ulNum < len(status.UnderlayNetworkList); ulNum++ {
		ulStatus := &status.UnderlayNetworkList[ulNum]
		networkID := ulStatus.Network
		// release the app number
		_, err := appNumOnUNetGet(ctx, networkID, appID)
		if err == nil {
			appNumOnUNetFree(ctx, networkID, appID)
		}
	}
}

// Assignable IP Address counter routines,
// for this network instance
func getAppCountOnUNet(ctx *zedrouterContext,
	netInstStatus *types.NetworkInstanceStatus) int {
	hostCount := 0
	if doNetworkInstanceStatusDhcpRangeSanityCheck(netInstStatus) == nil {
		hostCount = netInstStatus.DhcpRange.HostCount()
		subnetHostCount := types.GetIPAddrCountOnSubnet(netInstStatus.Subnet)
		// if hostCount for dhcp range is equal to or, more than subnet size,
		// deduct network, broadcast and gateway
		// and restrict it to AppNumMax
		if subnetHostCount >= 4 && hostCount >= subnetHostCount {
			subnetHostCount -= 3
			if hostCount > types.AppNumMax {
				hostCount = types.AppNumMax
			}
			netInstStatus.Gateway = types.AddToIP(netInstStatus.Subnet.IP, 1)
			netInstStatus.DhcpRange.Start =
				types.AddToIP(netInstStatus.Gateway, 1)
			netInstStatus.DhcpRange.End =
				types.AddToIP(netInstStatus.DhcpRange.Start, hostCount-1)
		}
	} else {
		if doNetworkInstanceSubnetSanityCheck(ctx, netInstStatus) == nil {
			hostCount = types.GetIPAddrCountOnSubnet(netInstStatus.Subnet)
			// deduct network, broadcast and gateway
			// TBD:XXX, subnet size less than 4!
			if hostCount >= 4 {
				hostCount -= 3
			}
		}
	}
	// default(Max) for switch networks or, bigger subnets
	// limited by Bitmap Size
	if hostCount == 0 || hostCount > types.AppNumMax {
		return types.AppNumMax
	}
	return hostCount
}
