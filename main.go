package main
// build with:
// $env:GOOS = 'linux'; $env:GOARCH = 'amd64'; go build -o C:\Users\suzuko\dev\installers\arch\main\bin\amd64; $env:GOOS = 'linux'; $env:GOARCH = 'arm64'; go build -o C:\Users\suzuko\dev\installers\arch\main\bin\arm64

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync"
	"time"
	"os"
	"os/signal"
	"syscall"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
	"github.com/insomniacslk/dhcp/dhcpv6"
	"github.com/insomniacslk/dhcp/dhcpv6/nclient6"
	"github.com/vishvananda/netlink"
)

const (
	// Linux include/uapi/linux/if_addr.h
	rtScopeLink       = 253 // Linux RT_SCOPE_LINK
	ifaFNODAD         = 0x02
	ifaFNOPREFIXROUTE = 0x200
)

type similarity struct {
	similarity float64
	index int
}

type dhcpInterface struct {
	iface    net.Interface
	sourceIP net.IP
}

type temporaryAddress struct {
	link netlink.Link
	addr *netlink.Addr
}

var (
	debugVal bool  = false
	debug    *bool = &debugVal

	tempAddrMu sync.Mutex
	tempAddrs  []temporaryAddress
)

func newLinkLocalAddress(iface net.Interface) (net.IP, error) {
	link, err := netlink.LinkByIndex(iface.Index)
	if err != nil {
		return nil, fmt.Errorf(
			"get link %q: %w",
			iface.Name,
			err,
		)
	}

	// Generate a random 64-bit interface ID.
	var iid [8]byte
	if _, err := rand.Read(iid[:]); err != nil {
		return nil, fmt.Errorf(
			"generate link-local IID for %q: %w",
			iface.Name,
			err,
		)
	}

	ip := make(net.IP, net.IPv6len)
	ip[0] = 0xfe
	ip[1] = 0x80
	copy(ip[8:], iid[:])

	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(64, 128),
		},

		// Linux RT_SCOPE_LINK.
		Scope: rtScopeLink,

		// Skip DAD and don't create another fe80::/64 route.
		Flags: ifaFNODAD | ifaFNOPREFIXROUTE,

		// Deprecated: don't use this address for ordinary
		// connections/source selection.
		PreferedLft: 0,

		// Keep it alive until we explicitly delete it.
		ValidLft: math.MaxUint32,
	}

	if err := netlink.AddrAdd(link, addr); err != nil {
		return nil, fmt.Errorf(
			"add temporary link-local %s to %q: %w",
			ip,
			iface.Name,
			err,
		)
	}

	tempAddrMu.Lock()
	tempAddrs = append(tempAddrs, temporaryAddress{
		link: link,
		addr: addr,
	})
	tempAddrMu.Unlock()

	if *debug {
		fmt.Printf(
			"→ temporary link-local %s on %s\n",
			ip,
			iface.Name,
		)
	}

	return ip, nil
}

func cleanupTemporaryAddresses() {
	tempAddrMu.Lock()
	addrs := tempAddrs
	tempAddrs = nil
	tempAddrMu.Unlock()

	// Reverse order is convenient if this ever gets expanded.
	for i := len(addrs) - 1; i >= 0; i-- {
		entry := addrs[i]

		if err := netlink.AddrDel(entry.link, entry.addr); err != nil {
			// E.g. interface disappeared before we got here.
			if *debug {
				fmt.Printf(
					"warning: remove temporary address %s from %s: %v\n",
					entry.addr.IP,
					entry.link.Attrs().Name,
					err,
				)
			}
			continue
		}

		if *debug {
			fmt.Printf(
				"→ removed temporary link-local %s from %s\n",
				entry.addr.IP,
				entry.link.Attrs().Name,
			)
		}
	}
}

func prepareInterfaces(
	chosen []net.Interface,
	newAddress bool,
) ([]dhcpInterface, error) {
	result := make([]dhcpInterface, 0, len(chosen))

	for _, iface := range chosen {
		entry := dhcpInterface{
			iface: iface,
		}

		if newAddress {
			ip, err := newLinkLocalAddress(iface)
			if err != nil {
				if *debug {
					fmt.Printf(
						"→ error adding temp address: %v\n",
						err,
					)
				}
				continue
			}
			entry.sourceIP = ip
		}

		result = append(result, entry)
	}

	return result, nil
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

func makeReq(
	ctx context.Context,
	optChan *chan []dhcpv6.Option,
	summChan *chan string,
	dhcpIf dhcpInterface,
	timeout time.Duration,
	retries int,
) {
	iface := dhcpIf.iface

	optTimeout := nclient6.WithTimeout(timeout)
	optRetry := nclient6.WithRetry(retries)

	opts := []nclient6.ClientOpt{optTimeout, optRetry}
	if *debug {
		opts = append(opts, nclient6.WithDebugLogger())
	}

	var (
		c   *nclient6.Client
		err error
	)

	if dhcpIf.sourceIP != nil {
		// Explicitly bind UDP/546 to the temporary link-local.
		conn, connErr := net.ListenUDP("udp6", &net.UDPAddr{
			IP:   dhcpIf.sourceIP,
			Port: dhcpv6.DefaultClientPort,
			Zone: iface.Name,
		})
		if connErr != nil {
			if *debug {
				fmt.Printf(
					"[%s] bind %s:546 failed: %v\n",
					iface.Name,
					dhcpIf.sourceIP,
					connErr,
				)
			}
			return
		}

		c, err = nclient6.NewWithConn(conn, iface.HardwareAddr, opts...)
		if err != nil {
			conn.Close()
		}
	} else {
		c, err = nclient6.New(iface.Name, opts...)
	}

	if err != nil {
		if *debug {
			fmt.Printf("[%s] DHCPv6 client creation failed: %v\n", iface.Name, err)
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

func reqTzdb(
	ctx context.Context,
	chosen []dhcpInterface,
	timeout time.Duration,
	retries int,
) (tzdbs [][]dhcpv6.Option) {
	total := len(chosen)
	tzdbChan := make(chan []dhcpv6.Option, total)
	summChan := make(chan string, total)

	var wg sync.WaitGroup

	for _, dhcpIf := range chosen {
		wg.Add(1)

		go func(dhcpIf dhcpInterface) {
			defer wg.Done()

			makeReq(
				ctx,
				&tzdbChan,
				&summChan,
				dhcpIf,
				timeout,
				retries,
			)
		}(dhcpIf)
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

func run() (err error) {
	debug = flag.Bool("debug", false, "debug")
	totalTime := flag.Bool("totalTime", false, "")
	multi := flag.Bool("multi", false, "print multiple tzs")
	doTzdb := flag.Bool("doTzdb", false, "print tzdb")
	doFqdn := flag.Bool("doFqdn", false, "print tzdb")
	newAddress := flag.Bool(
		"newAddress",
		false,
		"create and explicitly bind temporary link-local addresses",
		)

	flag.Parse()

	defer cleanupTemporaryAddresses()

    ifaces, err := net.Interfaces()
    if err != nil {
		return errors.New("failed to list interfaces: %v")
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
		return errors.New("no suitable interface found")
    }
	prepared, err := prepareInterfaces(chosen, *newAddress)
	if err != nil {
		return err
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

			tzdbs = reqTzdb(ctx, prepared, t, retries)


			if *debug {
				log.Printf("time of dhcpv6 req: %v\n", time.Since(st))
			}

			if len(tzdbs) > 0 {
				break
			}
			// break
		}

		if len(tzdbs) <= 0 {
			return errors.New("no tzdbs")
		}


		printTz(&tzdbs, multi)

	}

	if *totalTime || *debug {
		fmt.Println(time.Since(st))
	}

	if *doFqdn {
		fmt.Println("todo: implement later")
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


	return nil
}

func main() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		cleanupTemporaryAddresses()
		os.Exit(130)
	}()

	if err := run(); err != nil {
		log.Printf("error: %v", err)
	}
}

