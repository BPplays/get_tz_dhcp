package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/client6"
)

func printTz(tzdbs *[][]dhcpv6.Option, multi *bool) {


	if *multi{
		var tmp []string


		for i, tzdb := range *tzdbs {
			for i2 := range len(tzdb) {
				str := string((*tzdbs)[i][i2].ToBytes())
				tmp = append(tmp, str)
			}
		}

		fmt.Println(strings.Join(tmp, ","))

	} else {
		fmt.Println(string((*tzdbs)[0][0].ToBytes()))
	}

}

func main() {
	debug := flag.Bool("debug", false, "debug")
	multi := flag.Bool("multi", false, "print multiple tzs")
	flag.Parse()


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

		if *debug {
			fmt.Printf("→ using interface %q\n", chosen)
		}
    }
    if len(chosen) <= 0 {
        log.Fatal("no suitable interface found")
    }


	c := client6.NewClient()

	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone)
	// reqTzdb := dhcpv6.OptRequestedOption(dhcpv6.OptionNewTZDBTimezone)
	// fmt.Println(reqTzdb.String())


	st := time.Now()

	var tzdbs [][]dhcpv6.Option


	tzdbChan := make(chan []dhcpv6.Option, len(chosen))

	var wg sync.WaitGroup

	for _, iface := range chosen {
		wg.Add(1)


		go func(iface net.Interface) {
			defer wg.Done()

			sol, adv, err := c.Solicit(iface.Name, reqTzdb)
			if err != nil {
				return
				// log.Fatalf("Solicit failed: %v", err)
			}
			if *debug {
				fmt.Println(sol)
			}

			advMsg, ok := adv.(*dhcpv6.Message)
			if !ok {
				return
				// log.Fatalf("unexpected type %T, want *dhcpv6.Message", adv)
			}

			req, rep, err := c.Request(iface.Name, advMsg, reqTzdb)
			if *debug {
				fmt.Println(req, rep)
			}

			// tzdbs = append(tzdbs, rep.GetOption(dhcpv6.OptionNewTZDBTimezone))

			tzdbChan <- rep.GetOption(dhcpv6.OptionNewTZDBTimezone)

		}(iface)
	}

	wg.Wait()
	close(tzdbChan)

	for tzdb := range tzdbChan {
		tzdbs = append(tzdbs, tzdb)
	}

	if *debug {
		log.Printf("time of dhcpv6 req: %v\n", time.Since(st))
	}


	if len(tzdbs) <= 0 {
		log.Fatalln("no tzdbs")
	}

	printTz(&tzdbs, multi)


}

