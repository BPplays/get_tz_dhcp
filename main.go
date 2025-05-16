package main

import (
	"fmt"
	"log"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
)

func main() {
	c := client6.NewClient()

	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone)
	// reqTzdb := dhcpv6.OptRequestedOption(dhcpv6.OptionNewTZDBTimezone)
	// fmt.Println(reqTzdb.String())


	hwa, err := net.ParseMAC("18-C0-4D-89-04-1B")
	if err != nil {
		log.Fatalln(err)
	}

    // // 1. Build a Solicit asking only for option 42
    solicitMsg, err := dhcpv6.NewSolicit(hwa, reqTzdb)
    if err != nil {
        log.Fatalf("failed to build Solicit: %v", err)
    }

    advertiseMsg, err := dhcpv6.NewAdvertiseFromSolicit(solicitMsg)
    if err != nil {
        log.Fatalf("failed to build Solicit: %v", err)
    }

	// solicitMsg, advertiseMsg, err := c.Solicit("Ethernet", reqTzdb)
	// if err != nil {
	// 	log.Fatalf("Solicit failed: %v", err)
	// }
	// fmt.Println(solicitMsg, advertiseMsg)

	requestMsg, replyMsg, err := c.Request("Ethernet", advertiseMsg, reqTzdb)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	fmt.Println(requestMsg, replyMsg)

    // // 2. Send it on the wire (omitting socket setup for brevity)...
    // //    ...receive Advertise msg ...
    //
    // // 3. Build a Request from the Advertise, again asking for 42
    // requestMsg, err := dhcpv6.NewRequestFromAdvertise(advertiseMsg, reqTzdb)
    // if err != nil {
    //     log.Fatalf("failed to build Request: %v", err)
    // }
    //
    // // 4. Send and receive Reply, then extract TZDB:
    // tzdb := requestReply.Options().String(42)
    // log.Println("TZDB from server:", tzdb)
}

