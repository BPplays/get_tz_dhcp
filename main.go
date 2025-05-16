package main

import (
	"fmt"
	"log"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
)

func main() {


    ifaces, err := net.Interfaces()
    if err != nil {
        log.Fatalf("failed to list interfaces: %v", err)
    }

    var chosen []net.Interface
    for _, iface := range ifaces {
        if iface.Flags&net.FlagUp == 0 {
            continue
        }
        if iface.Flags&net.FlagLoopback != 0 {
            continue
        }
        chosen = append(chosen, iface)
        fmt.Printf("→ using interface %q\n", chosen)
    }
    if len(chosen) <= 0 {
        log.Fatal("no suitable interface found")
    }


	c := client6.NewClient()

	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone)
	// reqTzdb := dhcpv6.OptRequestedOption(dhcpv6.OptionNewTZDBTimezone)
	// fmt.Println(reqTzdb.String())



	var tzdbs [][]dhcpv6.Option
	for _, iface := range chosen {
		sol, adv, err := c.Solicit(iface.Name, reqTzdb)
		if err != nil {
			continue
			// log.Fatalf("Solicit failed: %v", err)
		}
		fmt.Println(sol)
		// Assert the interface to *dhcpv6.Message
		advMsg, ok := adv.(*dhcpv6.Message)
		if !ok {
			continue
			// log.Fatalf("unexpected type %T, want *dhcpv6.Message", adv)
		}

		// 'adv' now comes from an actual DHCPv6 server and includes a ServerID.
		req, rep, err := c.Request(iface.Name, advMsg, reqTzdb)
		fmt.Println(req, rep)

		tzdbs = append(tzdbs, rep.GetOption(dhcpv6.OptionNewTZDBTimezone))

	}


	if len(tzdbs) <= 0 {
		log.Fatalln("no tzdbs")
	}

	for i, tzdb := range tzdbs {
		for i2 := range len(tzdb) {
			fmt.Println(tzdbs[i][i2].ToBytes())
			str := string(tzdbs[i][i2].ToBytes())
			fmt.Println(str)
		}
	}



}

