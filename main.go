package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"runtime"
	"strings"
	"sync"
	"time"

	// "golang.org/x/net/ipv6"

	// "context"
	"errors"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/insomniacslk/dhcp/dhcpv6"

	// "github.com/insomniacslk/dhcp/iana"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	// "github.com/insomniacslk/dhcp/dhcpv6/client6"

	dbus "github.com/godbus/dbus/v5"
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

func NewInfoRequestFromAdvertise(adv *dhcpv6.Message, modifiers ...dhcpv6.Modifier) (*dhcpv6.Message, error) {
	if adv == nil {
		return nil, errors.New("ADVERTISE cannot be nil")
	}
	if adv.MessageType != dhcpv6.MessageTypeAdvertise {
		return nil, fmt.Errorf("The passed ADVERTISE must have ADVERTISE type set")
	}
	req, err := dhcpv6.NewMessage()
	if err != nil {
		return nil, err
	}
	req.MessageType = dhcpv6.MessageTypeInformationRequest
	cid := adv.GetOneOption(dhcpv6.OptionClientID)
	if cid == nil {
		return nil, fmt.Errorf("Client ID cannot be nil in ADVERTISE when building REQUEST")
	}
	req.AddOption(cid)
	sid := adv.GetOneOption(dhcpv6.OptionServerID)
	if sid == nil {
		return nil, fmt.Errorf("Server ID cannot be nil in ADVERTISE when building REQUEST")
	}
	req.AddOption(sid)
	req.AddOption(dhcpv6.OptElapsedTime(0))
	req.AddOption(dhcpv6.OptRequestedOption(
		dhcpv6.OptionDNSRecursiveNameServer,
		dhcpv6.OptionDomainSearchList,
	))

	// add OPTION_VENDOR_CLASS, only if present in the original request
	// TODO implement OptionVendorClass
	vClass := adv.GetOneOption(dhcpv6.OptionVendorClass)
	if vClass != nil {
		req.AddOption(vClass)
	}

	// apply modifiers
	for _, mod := range modifiers {
		mod(req)
	}
	return req, nil
}

func setTZLinux(zone string) error {
    conn, err := dbus.SystemBus()
    if err != nil {
        return err
    }
    obj := conn.Object("org.freedesktop.timedate1", "/org/freedesktop/timedate1")
    // signature "sb": string (TZ name), boolean (fix RTC)
    return obj.Call("org.freedesktop.timedate1.SetTimezone", 0, zone, false).Err
}

func setTZ(zone string) error {
	switch runtime.GOOS {
	case "linuxs":
		return setTZLinux(zone)

	default:
		return fmt.Errorf("error setting timezones on %v is an %w", runtime.GOOS, errors.ErrUnsupported)
	}
}

func NewIPv6UDPConn(ctx context.Context, iface string, port int) (net.PacketConn, error) {
	ip, err := dhcpv6.GetLinkLocalAddr(iface)
	if err != nil {
		return nil, err
	}

	return net.ListenUDP("udp6", &net.UDPAddr{
		IP:   ip,
		Port: port,
		Zone: iface,
	})
}
func newNclient6(ctx context.Context, iface string, port int, opts ...nclient6.ClientOpt) (*nclient6.Client, error) {
	c, err := NewIPv6UDPConn(ctx, iface, port)
	if err != nil {
		return nil, err
	}

	i, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, err
	}
	return nclient6.NewWithConn(c, i.HardwareAddr, opts...)
}

func makeReq(ctx context.Context, optChan *chan []dhcpv6.Option, summChan *chan string, iface net.Interface, timeout time.Duration, retries int) {
	// defer func() { fmt.Println("done req"); wg.Done() }()

	optTimeout := nclient6.WithTimeout(timeout)
	optRetry := nclient6.WithRetry(retries)

	opts := []nclient6.ClientOpt{optTimeout, optRetry}
	if *debug {
		opts = append(opts, nclient6.WithDebugLogger())
	}

	// optDebug := nclient6.WithDebugLogger()

	// fmt.Println("starting")
	c, err := newNclient6(ctx, iface.Name, dhcpv6.DefaultClientPort, opts...)
	if err != nil {
		if *debug {
			fmt.Println(err)
		}
		return
	}
	defer c.Close()




	mods := []dhcpv6.Modifier{}
	reqTzdb := dhcpv6.WithRequestedOptions(dhcpv6.OptionNewTZDBTimezone, dhcpv6.OptionFQDN)

	mods = append(mods, reqTzdb)

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionMIPv6IdentifiedHomeNetworkInformation))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionMIPv6UnrestrictedHomeNetworkInformation))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionMIPv6HomeNetworkPrefix))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionMIPv6HomeAgentAddress))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionMIPv6HomeAgentFQDN))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionV6PCPServer))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionV6Prefix64))

	mods = append(mods, dhcpv6.WithRequestedOptions(dhcpv6.OptionDNSRecursiveNameServer))


	// fmt.Println("getreqopt")
	adv, err := c.Solicit(ctx, mods...)
	if err != nil {
		if *debug {
			fmt.Println(err)
		}
		return
		// log.Fatalf("Solicit failed: %v", err)
	}
	// fmt.Println("getsol")

	advReq, err := NewInfoRequestFromAdvertise(adv, reqTzdb)
	if err != nil {
		return
	}
	// fmt.Println("getadvmsg")

	addr := net.UDPAddr{IP: dhcpv6.AllDHCPServers, Port: dhcpv6.DefaultServerPort}
	rep, err := c.SendAndRead(ctx, &addr, advReq, nil)
	if err != nil {
		if *debug {
			fmt.Println(err)
		}
		return
	}

	// fmt.Println("getrep")

	// c.SendAndRead()

	// rep, err := c.Request(ctx, adv, reqTzdb)
	// if err != nil {
	// 	fmt.Println(err)
	// }

	if *debug {
		// fmt.Println(rep)
		*summChan <- rep.Summary()
	}

	// tzdbs = append(tzdbs, rep.GetOption(dhcpv6.OptionNewTZDBTimezone))

	*optChan <- rep.GetOption(dhcpv6.OptionNewTZDBTimezone)
}

func reqTzdb(ctx context.Context, chosen []net.Interface, timeout time.Duration, retries int) (tzdbs [][]dhcpv6.Option) {
	total := len(chosen)
	tzdbChan := make(chan []dhcpv6.Option, total)

	summChan := make(chan string, total)

	var wg sync.WaitGroup

	for _, iface := range chosen {
		wg.Add(1)

		go func(ctx context.Context, iface net.Interface, timeout time.Duration, retries int) {
			defer wg.Done()

			makeReq(ctx, &tzdbChan, &summChan, iface, timeout, retries)
		}(ctx, iface, timeout, retries)
	}

	wg.Wait()
	close(tzdbChan)
	close(summChan)

	if *debug {
		for summ := range summChan {
			fmt.Println(summ)
		}
	}

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

func sprintTz(tzdbs *[][]dhcpv6.Option, multi *bool) string {
	var tzdbsString []string


	for i, tzdb := range *tzdbs {
		for i2 := range len(tzdb) {
			str := string((*tzdbs)[i][i2].ToBytes())
			tzdbsString = append(tzdbsString, str)
		}
	}

	if *multi{
		return strings.Join(tzdbsString, ",")
	} else {
		return sprintSingleTz(tzdbsString, 250)
	}

}

func main() {
	debug = flag.Bool("debug", false, "debug")
	totalTime := flag.Bool("totalTime", false, "")
	multi := flag.Bool("multi", false, "print multiple tzs")
	quiet := flag.Bool("q", false, "don't print tzs")
	doTzdb := flag.Bool("doTzdb", false, "print tzdb")
	doFqdn := flag.Bool("doFqdn", false, "print tzdb")

	setSysTZ := flag.Bool("setSysTZ", false, "sets the system timezone")

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



	timeouts := []time.Duration{650 * time.Millisecond, 1000 * time.Millisecond, 3000 * time.Millisecond}

	st := time.Now()

	if *doTzdb {
		var tzdbs [][]dhcpv6.Option
		// fix not closing socket. can i fix it?
		for _, t := range  timeouts {

			st := time.Now()
			retries := 3

			timeoutBuffer := 100 * time.Millisecond

			ctxTimeout := (t * time.Duration(retries)) + timeoutBuffer
			ctx, cancel := context.WithTimeout(context.Background(), ctxTimeout)
			defer cancel()
			if *debug {
				fmt.Println(t)
			}

			tzdbs = reqTzdb(ctx, chosen, t, retries)


			if *debug {
				log.Printf("time of dhcpv6 req: %v\n", time.Since(st))
			}

			if len(tzdbs) > 0 {
				break
			}
			// break
		}

		if len(tzdbs) <= 0 {
			log.Fatalln("no tzdbs")
		}


		tzS := sprintTz(&tzdbs, multi)
		if !*quiet {
			fmt.Println(tzS)
		}

		if *setSysTZ {
			if *multi {
				log.Fatalln("setting tz and multi are unsupported")
			}
			err := setTZ(tzS)
			if err != nil {
				fmt.Printf("couldn't set timezone %v", err)
			}

		}

	}

	if *totalTime || *debug {
		fmt.Println(time.Since(st))
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

