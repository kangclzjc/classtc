package main

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/florianl/go-tc"
	"github.com/florianl/go-tc/core"
	"github.com/jsimonetti/rtnetlink"
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

func getRoutedInterface() (*net.Interface, error) {
	return nettest.RoutedInterface("ip", net.FlagUp|net.FlagBroadcast)
}

// setupDummyInterface installs a temporary dummy interface
func setupDummyInterface(iface string) (*rtnetlink.Conn, error) {
	con, err := rtnetlink.Dial(nil)
	if err != nil {
		return &rtnetlink.Conn{}, err
	}

	if err := con.Link.New(&rtnetlink.LinkMessage{
		Family: unix.AF_UNSPEC,
		Type:   unix.ARPHRD_NETROM,
		Index:  0,
		Flags:  unix.IFF_UP,
		Change: unix.IFF_UP,
		Attributes: &rtnetlink.LinkAttributes{
			Name: iface,
			Info: &rtnetlink.LinkInfo{Kind: "dummy"},
		},
	}); err != nil {
		return con, err
	}

	return con, err
}

func ExampleTbf() {
	tcIface := "tcExampleTbf"

	rtnl, err := setupDummyInterface(tcIface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not setup dummy interface: %v\n", err)
		return
	}
	defer rtnl.Close()

	devID, err := net.InterfaceByName(tcIface)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get interface ID: %v\n", err)
		return
	}
	defer func(devID uint32, rtnl *rtnetlink.Conn) {
		if err := rtnl.Link.Delete(devID); err != nil {
			fmt.Fprintf(os.Stderr, "could not delete interface: %v\n", err)
		}
	}(uint32(devID.Index), rtnl)

	tcnl, err := tc.Open(&tc.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not open rtnetlink socket: %v\n", err)
		return
	}
	defer func() {
		if err := tcnl.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "could not close rtnetlink socket: %v\n", err)
		}
	}()

	linklayerEthernet := uint8(1)
	burst := uint32(0x500000)

	qdisc := tc.Object{
		Msg: tc.Msg{
			Family:  unix.AF_UNSPEC,
			Ifindex: uint32(devID.Index),
			Handle:  core.BuildHandle(tc.HandleRoot, 0x0),
			Parent:  tc.HandleRoot,
			Info:    0,
		},

		Attribute: tc.Attribute{
			Kind: "tbf",
			Tbf: &tc.Tbf{
				Parms: &tc.TbfQopt{
					Mtu:   1514,
					Limit: 0x5000,
					Rate: tc.RateSpec{
						Rate:      0x7d00,
						Linklayer: linklayerEthernet,
						CellLog:   0x3,
					},
				},
				Burst: &burst,
			},
		},
	}

	// tc qdisc add dev tcExampleTbf root tbf burst 20480 limit 20480 mtu 1514 rate 32000bps
	if err := tcnl.Qdisc().Add(&qdisc); err != nil {
		fmt.Fprintf(os.Stderr, "could not assign tbf to %s: %v\n", tcIface, err)
		return
	}
	defer func() {
		if err := tcnl.Qdisc().Delete(&qdisc); err != nil {
			fmt.Fprintf(os.Stderr, "could not delete tbf qdisc of %s: %v\n", tcIface, err)
			return
		}
	}()

	qdiscs, err := tcnl.Qdisc().Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get all qdiscs: %v\n", err)
		// return
	}

	fmt.Println("## qdiscs:")
	for _, qdisc := range qdiscs {

		iface, err := net.InterfaceByIndex(int(qdisc.Ifindex))
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not get interface from id %d: %v", qdisc.Ifindex, err)
			return
		}
		fmt.Printf("%20s\t%-11s\n", iface.Name, qdisc.Kind)
	}
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

func main() {
	rtnl, err := tc.Open(&tc.Config{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not open rtnetlink socket: %v\n", err)
		return
	}
	defer func() {
		if err := rtnl.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "could not close rtnetlink socket: %v\n", err)
		}
	}()
	qdiscs, err := rtnl.Qdisc().Get()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not get qdiscs: %v\n", err)
		return
	}
	ExampleTbf()

	for _, qdisc := range qdiscs {
		iface, err := net.InterfaceByIndex(int(qdisc.Ifindex))
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not get interface from id %d: %v", qdisc.Ifindex, err)
			return
		}
		fmt.Printf("%20s\t%s\n", iface.Name, qdisc.Kind)
	}

	// rifs := nettest.RoutedInterface("ip", net.FlagUp|net.FlagBroadcast)
	// if rifs != nil {
	// 	fmt.Println("Routed interface is ", rifs.HardwareAddr.String())
	// 	fmt.Println("Flags are", rifs.Flags.String())
	// }
	fmt.Println(getRoutedInterface())
	fmt.Println(getDefaultNIC())
}
