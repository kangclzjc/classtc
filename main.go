package main

import (
	"flag"
	"log"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type info struct {
	iface string
	bw    uint64
	sec   int64
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

	// qdisc
	// tc qdisc add dev lo root handle 1:0 htb default 1
	attrs := netlink.QdiscAttrs{
		LinkIndex: l.Attrs().Index,
		Handle:    netlink.MakeHandle(1, 0),
		Parent:    netlink.HANDLE_ROOT,
	}
	qdisc := netlink.NewHtb(attrs)
	err = netlink.QdiscAdd(qdisc)
	if err != nil {
		log.Fatalf("QdiskAdd error: %s\n", err)
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

	qdiscs, err := netlink.QdiscList(l)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Qdisk set done")
	log.Printf("%#v\n", qdiscs[0])
	time.Sleep(time.Duration(i.sec) * time.Second)

	// tc qdisc del dev lo root
	err = netlink.QdiscDel(qdisc)
	if err != nil {
		log.Fatalf("QdiskDel error: %s", err)
	}
	log.Println("Qdisc delete done")
	log.Println("Qdisc test end")
}
