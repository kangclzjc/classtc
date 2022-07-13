package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/net/nettest"
	"golang.org/x/sys/unix"
)

const (
	file  = "/proc/net/route"
	line  = 1    // line containing the gateway addr. (first line: 0)
	sep   = "\t" // field separator
	field = 2    // field containing hex gateway address (first field: 0)
)

var defaultRoute = [4]byte{0, 0, 0, 0}

type info struct {
	iface string
	bw    uint64
	sec   int64
}

func SafeQdiscList(link netlink.Link) ([]netlink.Qdisc, error) {
	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return nil, err
	}
	result := []netlink.Qdisc{}
	for _, qdisc := range qdiscs {
		// filter out pfifo_fast qdiscs because
		// older kernels don't return them
		_, pfifo := qdisc.(*netlink.PfifoFast)
		if !pfifo {
			result = append(result, qdisc)
		}
	}
	return result, nil
}

func getDefaultNIC() string {
	file, err := os.Open(file)
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()

	var eth0 string
	scanner := bufio.NewScanner(file)
	scanner.Scan()

	// jump to line containing the gateway address
	for i := 0; i < line; i++ {
		scanner.Scan()
	}

	// get field containing gateway address
	tokens := strings.Split(scanner.Text(), sep)
	eth0 = tokens[0]
	fmt.Println(tokens[0])
	gatewayHex := "0x" + tokens[field]

	// cast hex address to uint32
	d, _ := strconv.ParseInt(gatewayHex, 0, 64)
	d32 := uint32(d)

	// make net.IP address from uint32
	ipd32 := make(net.IP, 4)
	binary.LittleEndian.PutUint32(ipd32, d32)
	fmt.Printf("%T --> %[1]v\n", ipd32)

	// format net.IP to dotted ipV4 string
	ip := net.IP(ipd32).String()
	fmt.Printf("%T --> %[1]v\n", ip)
	return eth0
}

func getRoutedInterface() (*net.Interface, error) {
	return nettest.RoutedInterface("ip", net.FlagUp|net.FlagBroadcast)
}

func main() {
	i := &info{}
	flag.StringVar(&i.iface, "if", "lo", "select interface")
	flag.Uint64Var(&i.bw, "bw", 125000, "specify bandwidth in kbps")
	flag.Int64Var(&i.sec, "s", 10, "running second")
	flag.Parse()

	l, err := netlink.LinkByName(i.iface)
	if err != nil {
		panic(err)
	}
	log.Println("htb test start")

	qdiscs, err := SafeQdiscList(l)
	if err != nil {
		return
	}

	var htb *netlink.Htb
	var hasHtb bool = false
	for _, qdisc := range qdiscs {
		log.Printf("qdisc is %s\n", qdisc)

		h, isHTB := qdisc.(*netlink.Htb)
		if isHTB {
			htb = h
			hasHtb = true
			break
		}
	}
	if !hasHtb {
		// qdisc
		// tc qdisc add dev lo root handle 1:0 htb default 1
		attrs := netlink.QdiscAttrs{
			LinkIndex: l.Attrs().Index,
			Handle:    netlink.MakeHandle(1, 0),
			Parent:    netlink.HANDLE_ROOT,
		}
		htb = netlink.NewHtb(attrs)
		err = netlink.QdiscAdd(htb)
		if err != nil {
			log.Fatalf("QdiscAdd error: %s\n", err)
		}
	}

	// htb parent class
	// tc class add dev lo parent 1:0 classid 1:1 htb rate 125Mbps ceil 125Mbps prio 0
	// preconfig
	classattrs1 := netlink.ClassAttrs{
		LinkIndex: l.Attrs().Index,
		Parent:    netlink.MakeHandle(1, 0),
		Handle:    netlink.MakeHandle(1, 1),
	}
	htbclassattrs1 := netlink.HtbClassAttrs{
		Rate:    10000000000,
		Cbuffer: 0,
	}
	class1 := netlink.NewHtbClass(classattrs1, htbclassattrs1)
	if err := netlink.ClassAdd(class1); err != nil {
		log.Fatal(err)
	}

	// htb child class
	// tc class add dev lo parent 1:0 classid 1:5 htb rate 125kbps ceil 250kbps prio 0
	classattrs2 := netlink.ClassAttrs{
		LinkIndex: l.Attrs().Index,
		Parent:    netlink.MakeHandle(1, 0),
		Handle:    netlink.MakeHandle(1, 5),
	}
	htbclassattrs2 := netlink.HtbClassAttrs{
		Rate:    i.bw,
		Cbuffer: uint32(i.bw) * 2,
	}
	class2 := netlink.NewHtbClass(classattrs2, htbclassattrs2)
	if err := netlink.ClassAdd(class2); err != nil {
		log.Fatal(err)
	}

	// filter add
	// tc filter add dev lo parent 1:0 prio 0 protocol all handle 5 fw flowid 1:5
	filterattrs := netlink.FilterAttrs{
		LinkIndex: l.Attrs().Index,
		Parent:    netlink.MakeHandle(1, 0),
		Handle:    netlink.MakeHandle(0, 1024),
		Priority:  49152,
		Protocol:  unix.ETH_P_ALL,
	}

	fwattrs := netlink.FilterFwAttrs{
		ClassId: netlink.MakeHandle(1, 5),
	}

	filter, err := netlink.NewFw(filterattrs, fwattrs)
	if err != nil {
		log.Printf("failed to create NewFw(). Reason:%s", err)
	}

	if err := netlink.FilterAdd(filter); err != nil {
		log.Printf("failed to add filter. Reason:%s", err)
	}

	qdiscs, err = netlink.QdiscList(l)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Qdisk set done")
	log.Printf("%#v\n", qdiscs[0])
	time.Sleep(time.Duration(i.sec) * time.Second)

	// tc qdisc del dev lo root
	//err = netlink.QdiscDel(htb)
	//if err != nil {
	//	log.Fatalf("QdiskDel error: %s", err)
	//}
	log.Println("Qdisc delete done")
	log.Println("Qdisc test end")
	log.Println(getDefaultNIC())
	log.Println(getRoutedInterface())
}
