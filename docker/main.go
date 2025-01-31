package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/David-VTUK/KubePlumber/common"
)

func main() {

	var networkInterfaces common.NetworkInterfaces

	interfaces, err := net.Interfaces()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, iface := range interfaces {

		// Filter rout loopback interfaces
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		networkInterfaces.Interfaces = append(networkInterfaces.Interfaces, common.NetworkInterface{
			Name:         iface.Name,
			MAC:          iface.HardwareAddr.String(),
			MTU:          iface.MTU,
			Up:           iface.Flags&net.FlagUp != 0,
			Broadcast:    iface.Flags&net.FlagBroadcast != 0,
			Loopback:     iface.Flags&net.FlagLoopback != 0,
			PointToPoint: iface.Flags&net.FlagPointToPoint != 0,
			Multicast:    iface.Flags&net.FlagMulticast != 0,
			Running:      iface.Flags&net.FlagRunning != 0,
		})
	}

	jsonData, err := json.Marshal(networkInterfaces)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println(string(jsonData))
	os.Exit(0)
}
