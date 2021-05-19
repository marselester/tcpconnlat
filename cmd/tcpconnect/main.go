// +build linux

package main

import (
	"bytes"
	"encoding/binary"
	"log"
	"os"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/perf"
	"golang.org/x/sys/unix"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cflags $BPF_CFLAGS -cc clang-11 TCPConnect ./bpf/tcpconnect.bpf.c -- -I../../headers

func main() {
	// Increase the rlimit of the current process to provide sufficient space
	// for locking memory for the eBPF map.
	if err := unix.Setrlimit(unix.RLIMIT_MEMLOCK, &unix.Rlimit{
		Cur: unix.RLIM_INFINITY,
		Max: unix.RLIM_INFINITY,
	}); err != nil {
		log.Fatalf("failed to set temporary rlimit: %v", err)
	}

	objs := TCPConnectObjects{}
	if err := LoadTCPConnectObjects(&objs, nil); err != nil {
		log.Fatalf("failed to load objects: %v", err)
	}
	defer objs.Close()

	tcpv4kp, err := link.Kprobe("tcp_v4_connect", objs.TCPConnectPrograms.TcpV4Connect)
	if err != nil {
		log.Fatalf("opening tcp_v4_connect kprobe: %s", err)
	}
	defer tcpv4kp.Close()

	tcpv4krp, err := link.Kretprobe("tcp_v4_connect", objs.TCPConnectPrograms.TcpV4ConnectRet)
	if err != nil {
		log.Fatalf("opening tcp_v4_connect kretprobe: %s", err)
	}
	defer tcpv4krp.Close()

	tcpv6kp, err := link.Kprobe("tcp_v6_connect", objs.TCPConnectPrograms.TcpV6Connect)
	if err != nil {
		log.Fatalf("opening tcp_v6_connect kprobe: %s", err)
	}
	defer tcpv6kp.Close()

	tcpv6krp, err := link.Kretprobe("tcp_v6_connect", objs.TCPConnectPrograms.TcpV6ConnectRet)
	if err != nil {
		log.Fatalf("opening tcp_v6_connect kretprobe: %s", err)
	}
	defer tcpv6krp.Close()

	log.Println("Waiting for events...")

	// Open a perf event reader from userspace on the PERF_EVENT_ARRAY map
	// defined in the BPF C program.
	rd, err := perf.NewReader(objs.TCPConnectMaps.Events, os.Getpagesize())
	if err != nil {
		log.Fatalf("creating perf event reader: %s", err)
	}
	defer rd.Close()

	for {
		var v event

		record, err := rd.Read()
		if err != nil {
			if perf.IsClosed(err) {
				return
			}
			log.Printf("reading from perf event reader: %v", err)
		}

		if record.LostSamples != 0 {
			log.Printf("ring event perf buffer is full, dropped %d samples", record.LostSamples)
			continue
		}

		err = binary.Read(
			bytes.NewBuffer(record.RawSample),
			binary.LittleEndian,
			&v,
		)
		if err != nil {
			log.Printf("failed to parse perf event: %v", err)
			continue
		}

		log.Println(v)
	}
}

// event represents a perf event sent to userspace from the BPF program running in the kernel.
// Note, that it must match the C event struct, and both C and Go structs must be aligned the same way.
type event struct {
	saddrV4 uint32
	saddrV6 [16]byte
	daddrV4 uint32
	daddrV6 [16]byte
	comm    [16]byte
	tsUs    uint64
	af      int
	pid     uint32
	uid     uint32
	dport   uint16
}