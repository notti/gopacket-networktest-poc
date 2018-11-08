package main

import (
	"bufio"
	"bytes"
	"debug/elf"
	"encoding/binary"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"

	"github.com/cavaliercoder/go-cpio"
)

const (
	initFile     = "init/test.go"
	kernelModule = "PF_RING/kernel/pf_ring.ko"
	kernelFile   = "linux-4.9.135/arch/x86_64/boot/bzImage"
)

func clen(n []byte) int {
	for i := 0; i < len(n); i++ {
		if n[i] == 0 {
			return i
		}
	}
	return len(n)
}

func getInterp(fname string) (string, error) {
	binary, err := elf.Open(fname)
	if err != nil {
		return "", nil
	}
	for _, prog := range binary.Progs {
		if prog.Type != elf.PT_INTERP {
			continue
		}
		tmp := make([]byte, prog.Filesz)
		n, err := prog.ReadAt(tmp, 0)
		if err != nil {
			log.Fatalln("Error during determining interp:", err)
		}
		if n != int(prog.Filesz) {
			log.Fatalln("Couldn't read interp fully")
		}
		return string(tmp[:clen(tmp)]), nil
	}
	return "", nil
}

func parseLibs(out []byte) (ret []string) {
	lines := bytes.Split(out, []byte("\n"))
	for _, line := range lines {
		splitline := bytes.SplitN(line, []byte("=>"), 2)
		if len(splitline) != 2 {
			continue
		}
		libline := bytes.TrimSpace(splitline[1])
		lib := string(libline[:bytes.IndexByte(libline, ' ')])
		ret = append(ret, lib)
	}
	return ret
}

func getLibsLdSO(interp, fname string) (ret []string, err error) {
	interpOut, err := exec.Command(interp, "--list", fname).Output()
	if err != nil {
		return
	}
	return parseLibs(interpOut), nil
}

func getLibsLdd(fname string) (ret []string, err error) {
	interpOut, err := exec.Command("ldd", fname).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			if exit.Sys().(syscall.WaitStatus).ExitStatus() == 1 {
				log.Println("   Not a dynamic lib/executable")
				return nil, nil
			}
		}
		return
	}
	return parseLibs(interpOut), nil
}

func handleNetwork(conn *os.File) {
	plen := make([]byte, 4)
	buf := make([]byte, 4096)
	for {
		_, err := io.ReadFull(conn, plen)
		if err != nil {
			return
		}
		n := binary.BigEndian.Uint32(plen)
		if cap(buf) < int(n) {
			buf = make([]byte, n)
		} else {
			buf = buf[:n]
		}
		_, err = io.ReadFull(conn, buf)
		if err != nil {
			return
		}
		log.Println("received: ", gopacket.NewPacket(buf, layers.LayerTypeEthernet, gopacket.NoCopy).Dump())

		out := gopacket.NewSerializeBuffer()
		opt := gopacket.SerializeOptions{
			FixLengths:       true,
			ComputeChecksums: true,
		}

		eth := layers.Ethernet{
			SrcMAC:       net.HardwareAddr{6, 5, 4, 3, 2, 1},
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

		err = gopacket.SerializeLayers(out, opt,
			&eth,
			&ip4,
			&udp,
			gopacket.Payload([]byte{'t', 'e', 's', 't'}))
		if err != nil {
			log.Fatal("Could not packet stuff together: ", err)
		}
		packetData := out.Bytes()
		log.Println("host: sending: ", gopacket.NewPacket(packetData, layers.LayerTypeEthernet, gopacket.NoCopy).Dump())
		binary.BigEndian.PutUint32(plen, uint32(len(packetData)))
		_, err = conn.Write(plen)
		if err != nil {
			log.Fatal("Could not send length")
		}
		_, err = conn.Write(packetData)
		if err != nil {
			log.Fatal("Could not send packet")
		}
	}
}

func addFile(w *cpio.Writer, name, file string) {
	if name[:1] == "/" {
		name = string(name[1:])
	}
	dirs := strings.Split(name, "/")
	for i := range dirs[:len(dirs)-1] {
		dir := strings.Join(dirs[:i+1], "/")
		if _, ok := writtenPaths[dir]; !ok {
			h := &cpio.Header{
				Name: dir,
				Size: 0,
				Mode: 040555,
			}
			err := w.WriteHeader(h)
			if err != nil {
				log.Fatal(err)
			}
			writtenPaths[dir] = struct{}{}
		}
	}

	f, err := os.Open(file)
	if err != nil {
		log.Fatalf("Couldn't open %s: %s\n", file, err)
	}
	defer f.Close()
	fStat, err := f.Stat()
	if err != nil {
		log.Fatalf("Couldn't examine %s: %s\n", file, err)
	}
	hdr, err := cpio.FileInfoHeader(fStat, "")
	hdr.Name = name
	err = w.WriteHeader(hdr)
	if err != nil {
		log.Fatalf("Error writing initrd header for %s: %s\n", name, err)
	}
	_, err = io.Copy(w, f)
	if err != nil {
		log.Fatalf("Couldn't add %s to initrd: %s\n", name, err)
	}
}

var writtenPaths map[string]struct{}

func main() {
	writtenPaths = make(map[string]struct{})
	initBinary, err := ioutil.TempFile("", "")
	if err != nil {
		log.Fatal("Couldn't create tempfile for binary: ", err)
	}
	fname := initBinary.Name()
	defer os.Remove(fname)
	initBinary.Close()
	build := exec.Command("go", "build", "-o", fname, initFile)
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	err = build.Run()
	if err != nil {
		log.Fatal("Couldn't build guest binary: ", err)
	}

	initRd, err := ioutil.TempFile("", "")
	if err != nil {
		log.Fatal("Couldn't create tempfile for initrd: ", err)
	}
	defer initRd.Close()
	defer os.Remove(initRd.Name())

	var libs []string

	interp, err := getInterp(fname)
	if err != nil {
		log.Fatalln("Couldn't determine interp:", err)
	}
	if interp == "" {
		l, err := getLibsLdd(fname)
		if err != nil {
			log.Fatalln("Couldn't get libs:", err)
		}
		libs = l
	} else {
		l, err := getLibsLdSO(interp, fname)
		if err != nil {
			log.Fatalln("Couldn't get libraries:", err)
		}
		libs = l
	}

	w := cpio.NewWriter(initRd)
	h := &cpio.Header{
		Name: ".",
		Size: 0,
		Mode: 040555,
	}
	err = w.WriteHeader(h)
	if err != nil {
		log.Fatal(err)
	}
	addFile(w, "init", fname)
	addFile(w, "pf_ring.ko", kernelModule)
	if interp != "" {
		addFile(w, string(interp[1:]), interp)
	}
	for _, lib := range libs {
		addFile(w, path.Join("usr/lib", path.Base(lib)), lib)
	}

	err = w.Close()
	if err != nil {
		log.Fatal("Error finishing initrd: ", err)
	}

	os.Remove(fname)

	netFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		log.Fatal("Could not create socket pair")
	}
	netMaster := os.NewFile(uintptr(netFds[0]), "netMaster")
	go handleNetwork(netMaster)

	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err != nil {
		log.Fatal("Could not create socket pair")
	}
	ctrl := os.NewFile(uintptr(fds[0]), "ctrl")
	ctrlReader := bufio.NewReader(ctrl)
	go func() {
		for {
			data, err := ctrlReader.ReadString('\n')
			if err != nil {
				log.Fatal("Error during ctrl-read: ", err)
			}
			log.Println("Got ctrl data: ", data)
			_, err = ctrl.WriteString("yay\n")
			if err != nil {
				log.Fatal("Error during ctrl-write: ", err)
			}
		}
	}()

	runner := exec.Command("qemu-system-x86_64",
		"-m", "500M",
		"-vga", "none",
		"-nographic",
		"-nodefaults",
		"-nodefconfig",
		"-no-user-config",
		"-serial", "none",
		"-kernel", kernelFile,
		"-initrd", initRd.Name(),
		"-append", "console=hvc0",
		"-device", "virtio-serial",
		"-chardev", "stdio,id=tty", "-device", "virtconsole,chardev=tty",
		"-add-fd", "set=1,fd=3",
		"-chardev", "pipe,path=/dev/fdset/1,id=ctrl", "-device", "virtserialport,chardev=ctrl",
		"-netdev", "socket,id=net0,fd=4", "-device", "virtio-net-pci,netdev=net0",
		"-no-reboot")
	runner.Stdout = os.Stdout
	runner.Stderr = os.Stderr
	runner.ExtraFiles = []*os.File{os.NewFile(uintptr(fds[1]), "slave"), os.NewFile(uintptr(netFds[1]), "netSlave")}
	err = runner.Run()
	if err != nil {
		log.Fatal("Error running tester: ", err)
	}
}
