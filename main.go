package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	// "golang.org/x/net/ipv6"

	// "context"
	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/insomniacslk/dhcp/dhcpv6"
	// "github.com/insomniacslk/dhcp/iana"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
)

var (
	debugVal bool = false
	debug *bool = &debugVal
)

type similarity struct {
	similarity float64
	index int
}

func StringSimilarity(s1 string, s2 string) (similarity float64) {
	sd := metrics.NewSorensenDice()
	similarity = strutil.Similarity(s1, s2, sd)
	return similarity
}


// func reqFqdn(chosen []net.Interface) (tzdbs [][]dhcpv6.Option) {
// 	c := client6.NewClient()
//
// 	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionFQDN)
//
//
//
//
//
// 	tzdbChan := make(chan []dhcpv6.Option, len(chosen))
//
// 	var wg sync.WaitGroup
//
// 	for _, iface := range chosen {
// 		wg.Add(1)
//
//
// 		go func(iface net.Interface) {
// 			defer wg.Done()
//
// 			// sol, adv, err := c.Solicit(iface.Name, reqTzdb)
// 			// if err != nil {
// 			// 	return
// 			// 	// log.Fatalf("Solicit failed: %v", err)
// 			// }
//
// 			msg, err := dhcpv6.NewMessage(reqTzdb)
// 			if err != nil {
// 				return
// 			}
// 			msg.MessageType = dhcpv6.MessageTypeInformationRequest
// 			msg.AddOption(dhcpv6.OptInformationRefreshTime(1000 * time.Second))
// 			// 4. Serialize
// 			b := msg.ToBytes()
//
// 			// 5. Send via UDP on port 547 to “ff02::1:2” (all-DHCP-servers multicast)
// 			raw, err := ipv6.ListenPacket(net.InterfaceByName(ifaceName), &net.UDPAddr{
// 				IP:   net.ParseIP("ff02::1:2"),
// 				Port: dhcpv6.DefaultServerPort,
// 			})
// 			if err != nil {
// 				return
// 			}
// 			defer raw.Close()
//
// 			_, err = raw.WriteTo(b, nil, &net.UDPAddr{IP: net.ParseIP("ff02::1:2"), Port: dhcpv6.DefaultServerPort})
//
// 			req, rep, err := c.Request(iface.Name, msg, reqTzdb)
//
// 			if *debug {
// 				fmt.Println(req, rep)
// 			}
//
// 			// tzdbs = append(tzdbs, rep.GetOption(dhcpv6.OptionNewTZDBTimezone))
//
// 			// fmt.Println(string(rep.ToBytes()))
// 			fmt.Println(rep.Summary())
// 			tzdbChan <- rep.GetOption(dhcpv6.OptionFQDN)
//
// 		}(iface)
// 	}
//
// 	wg.Wait()
// 	close(tzdbChan)
//
// 	for tzdb := range tzdbChan {
// 		tzdbs = append(tzdbs, tzdb)
// 	}
//
//
// 	return tzdbs
// }


func reqTzdb(ctx context.Context, chosen []net.Interface) (tzdbs [][]dhcpv6.Option) {
	tzdbChan := make(chan []dhcpv6.Option, len(chosen))

	var wg sync.WaitGroup

	for _, iface := range chosen {
		wg.Add(1)

		g, ctx := errgroup.WithContext(ctx)
		go func(ctx context.Context, iface net.Interface) {
			defer wg.Done()


			c, err := nclient6.New(iface.Name)
			if err != nil {
				if *debug {
					fmt.Println(err)
				}
				return
			}

			reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone)
			adv, err := c.Solicit(ctx, reqTzdb)
			if err != nil {
				if *debug {
					fmt.Println(err)
				}
				return
				// log.Fatalf("Solicit failed: %v", err)
			}

			advReq, err := dhcpv6.NewRequestFromAdvertise(adv)
			if err != nil {
				return
			}

			advReq.MessageType = dhcpv6.MessageTypeInformationRequest
			addr := net.UDPAddr{IP: dhcpv6.AllDHCPServers, Port: dhcpv6.DefaultServerPort}
			rep, err := c.SendAndRead(ctx, &addr, advReq, nil)
			if err != nil {
				if *debug {
					fmt.Println(err)
				}
				return
			}

			// c.SendAndRead()

			// rep, err := c.Request(ctx, adv, reqTzdb)
			// if err != nil {
			// 	fmt.Println(err)
			// }

			if *debug {
				fmt.Println(rep)
			}
			fmt.Println(rep.Summary())

			// tzdbs = append(tzdbs, rep.GetOption(dhcpv6.OptionNewTZDBTimezone))

			tzdbChan <- rep.GetOption(dhcpv6.OptionNewTZDBTimezone)

		}(ctx, iface)
	}

	wg.Wait()
	close(tzdbChan)

	for tzdb := range tzdbChan {
		tzdbs = append(tzdbs, tzdb)
	}


	return tzdbs
}

// get the string most similer to all the others
//
// if you put in > maxSize strings it just returns [0]
//
// maxSize should probably be ~250
func sprintSingleTz(stringsl []string, maxSize int) string {

	switch {
	case len(stringsl) <= 0:
		return ""
	case len(stringsl) <= 1:
		return stringsl[0]
	case len(stringsl) > maxSize && maxSize > -1:
		return stringsl[0]
	}

	var wg sync.WaitGroup
	sims := make(chan similarity, len(stringsl) * (len(stringsl)-1))

	for i := range stringsl {
		wg.Add(1)
		go func(strs []string, i int) {
			defer wg.Done()

			sim := similarity{similarity: 0, index: i}
			for i2 := range stringsl {
				if i2 == i {
					continue
				}
				sim = similarity{similarity: sim.similarity + StringSimilarity(stringsl[i], stringsl[i2]), index: i}
			}
			sims <- sim
		}(stringsl, i)
	}

	wg.Wait()
	close(sims)

	maxSim := similarity{similarity: -1.0, index: 0}
	for sim := range sims {
		if *debug {
			fmt.Println(sim.similarity, stringsl[sim.index])
		}
		// fmt.Println(sim.similarity, stringsl[sim.index])
		if sim.similarity > maxSim.similarity {
			maxSim = sim
		}
	}
	// fmt.Println(maxSim.similarity)

	if maxSim.similarity > -0.5 {
		return stringsl[maxSim.index]
	}
	return stringsl[0]
}

func printTz(tzdbs *[][]dhcpv6.Option, multi *bool) {
	var tzdbsString []string


	for i, tzdb := range *tzdbs {
		for i2 := range len(tzdb) {
			str := string((*tzdbs)[i][i2].ToBytes())
			tzdbsString = append(tzdbsString, str)
		}
	}

	if *multi{

		fmt.Println(strings.Join(tzdbsString, ","))

	} else {
		// fmt.Println(string((*tzdbs)[0][0].ToBytes()))
		fmt.Println(sprintSingleTz(tzdbsString, 250))
	}

}

func main() {
	debug = flag.Bool("debug", false, "debug")
	multi := flag.Bool("multi", false, "print multiple tzs")
	doTzdb := flag.Bool("doTzdb", false, "print tzdb")
	doFqdn := flag.Bool("doFqdn", false, "print tzdb")
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


	// reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionFQDN)
	// reqTzdb := dhcpv6.OptRequestedOption(dhcpv6.OptionNewTZDBTimezone)
	// fmt.Println(reqTzdb.String())


	st := time.Now()

	if *doTzdb {
		ctx, cancel := context.WithTimeout(context.Background(), 3000 * time.Second)
		defer cancel()

		tzdbs := reqTzdb(ctx, chosen)

		if len(tzdbs) <= 0 {
			log.Fatalln("no tzdbs")
		}

		if *debug {
			log.Printf("time of dhcpv6 req: %v\n", time.Since(st))
		}


		if len(tzdbs) <= 0 {
			log.Fatalln("no tzdbs")
		}

		printTz(&tzdbs, multi)

	}

	if *doFqdn {
		fmt.Println("fdhs")
	}

	// if *doFqdn {
	// 	fqdns, err := SendDHCPv6Requests(chosen, dhcpv6.MessageType(11), 3000 * time.Millisecond)
	// 	if err != nil {
	// 		log.Fatalln(err)
	// 	}
	//
	// 	if len(fqdns) <= 0 {
	// 		log.Fatalln("no fqdns")
	// 	}
	//
	// 	if *debug {
	// 		log.Printf("time of dhcpv6 req: %v\n", time.Since(st))
	// 	}
	//
	//
	// 	if len(fqdns) <= 0 {
	// 		log.Fatalln("no fqdns")
	// 	}
	//
	// 	fmt.Println(fqdns)
	// 	// printTz(&fqdns, multi)
	// }


}

