package main

import (
	"bufio"
	"io/ioutil"
	"log"
	"net"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/google/gopacket"

	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pfring"
	"github.com/vishvananda/netlink"
)

func main() {
	if err := unix.Mount("", "/dev", "devtmpfs", uintptr(0), ""); err != nil {
		log.Fatal("Failed mounting devtmpfs")
	}
	if test, err := os.OpenFile("/dev/vport0p1", os.O_RDWR, 0); err != nil {
		log.Fatal("Couldn't open stuff: ", err)
	} else {
		if _, err := test.WriteString("test\n"); err != nil {
			log.Fatal("Could not write stuff: ", err)
		}
		testReader := bufio.NewReader(test)
		if res, err := testReader.ReadString('\n'); err != nil {
			log.Fatal("Could not read stuff: ", err)
		} else {
			log.Println("[GUEST] got: ", res)
		}
		test.Close()
	}
	log.Println("[GUEST] Hello world")
	mod, err := os.Open("pf_ring.ko")
	if err != nil {
		log.Fatal("failed opening stuff: ", err)
	}
	image, err := ioutil.ReadAll(mod)
	if err != nil {
		log.Fatal("failed loading stuff: ", err)
	}
	opts := []byte{0}
	_, _, e := unix.Syscall(unix.SYS_INIT_MODULE, uintptr(unsafe.Pointer(&image[0])), uintptr(len(image)), uintptr(unsafe.Pointer(&opts[0])))
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
	log.Println("[GUEST] ", ifaces, err)
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
	log.Println("[GUEST] ", gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy).Dump())
	err = ring.WritePacketData(packetData)
	if err != nil {
		log.Fatal("error sending packet: ", err)
	}
	data, ci, err := ring.ZeroCopyReadPacketData()
	if err != nil {
		log.Fatal("error receiving packet: ", err)
	}
	log.Println("[GUEST] answer: ", ci, "\n", gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.NoCopy).Dump())
	log.Println("[GUEST] finished")
	unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
}
