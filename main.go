package main

import (
	"fmt"
	"log"
	// "net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
)

func main() {
	c := client6.NewClient()

	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone)
	// reqTzdb := dhcpv6.OptRequestedOption(dhcpv6.OptionNewTZDBTimezone)
	// fmt.Println(reqTzdb.String())



	sol, adv, err := c.Solicit("Ethernet", reqTzdb)
	if err != nil {
		log.Fatalf("Solicit failed: %v", err)
	}
	fmt.Println(sol)
	// Assert the interface to *dhcpv6.Message
	advMsg, ok := adv.(*dhcpv6.Message)
	if !ok {
		log.Fatalf("unexpected type %T, want *dhcpv6.Message", adv)
	}

	// 'adv' now comes from an actual DHCPv6 server and includes a ServerID.
	req, rep, err := c.Request("Ethernet", advMsg, reqTzdb)
	fmt.Println(req, rep)

	tzdbs := rep.GetOption(dhcpv6.OptionNewTZDBTimezone)
	for i := range len(tzdbs) {
		fmt.Println(tzdbs[i].ToBytes())
		str := string(tzdbs[i].ToBytes())
		fmt.Println(str)
	}


}

