package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"syscall"
	"time"
	"unsafe"

	"github.com/google/gopacket"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pfring"
	"github.com/vishvananda/netlink"
)

func main() {
	fmt.Println("Hello world")
	mod, err := os.Open("pf_ring.ko")
	if err != nil {
		log.Fatal("failed opening stuff: ", err)
	}
	image, err := ioutil.ReadAll(mod)
	if err != nil {
		log.Fatal("failed loading stuff: ", err)
	}
	log.Println(len(image))
	opts := []byte{0}
	_, _, e := syscall.Syscall(syscall.SYS_INIT_MODULE, uintptr(unsafe.Pointer(&image[0])), uintptr(len(image)), uintptr(unsafe.Pointer(&opts[0])))
	if e != 0 {
		log.Fatal("failed initing stuff: ", e.Error())
	}
	mod.Close()
	link, err := netlink.LinkByIndex(2)
	if err != nil {
		log.Fatal("failed doing link stuff: ", err)
	}
	err = netlink.LinkSetUp(link)
	if err != nil {
		log.Fatal("failed doing more link stuff: ", err)
	}
	addr, _ := netlink.ParseAddr("192.168.0.2/24")
	err = netlink.AddrAdd(link, addr)
	if err != nil {
		log.Fatal("failed adding address to link: ", err)
	}
	ifaces, err := net.Interfaces()
	fmt.Println(ifaces, err)
	ring, err := pfring.NewRing("eth0", 65536, pfring.FlagPromisc)
	if err != nil {
		log.Fatal("could not ring around: ", err)
	}
	err = ring.SetDirection(pfring.ReceiveOnly)
	if err != nil {
		log.Fatal("dir not working: ", err)
	}
	err = ring.SetSocketMode(pfring.WriteAndRead)
	if err != nil {
		log.Fatal("mode not working: ", err)
	}
	err = ring.Enable()
	if err != nil {
		log.Fatal("enable not working: ", err)
	}

	buf := gopacket.NewSerializeBuffer()
	opt := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	eth := layers.Ethernet{
		SrcMAC:       ifaces[1].HardwareAddr,
		DstMAC:       net.HardwareAddr{1, 2, 3, 4, 5, 6},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip4 := layers.IPv4{
		Version:  4,
		SrcIP:    net.IPv4(192, 168, 0, 2),
		DstIP:    net.IPv4(192, 168, 0, 1),
		Protocol: layers.IPProtocolUDP,
		TTL:      100,
	}
	udp := layers.UDP{
		SrcPort: 1,
		DstPort: 1,
	}
	udp.SetNetworkLayerForChecksum(&ip4)

	err = gopacket.SerializeLayers(buf, opt,
		&eth,
		&ip4,
		&udp,
		gopacket.Payload([]byte{1, 2, 3, 4}))
	if err != nil {
		log.Fatal("Could not packet stuff together: ", err)
	}
	packetData := buf.Bytes()
	log.Println(gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy).Dump())
	err = ring.WritePacketData(packetData)
	if err != nil {
		log.Fatal("error sending packet: ", err)
	}
	time.Sleep(3 * time.Second)
	syscall.Reboot(syscall.LINUX_REBOOT_CMD_RESTART)
}
