//go:build windows

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

var (
	iphlpapi        = syscall.NewLazyDLL("iphlpapi.dll")
	icmpCreateFile  = iphlpapi.NewProc("IcmpCreateFile")
	icmp6CreateFile = iphlpapi.NewProc("Icmp6CreateFile")
	icmpCloseHandle = iphlpapi.NewProc("IcmpCloseHandle")
	icmpSendEcho    = iphlpapi.NewProc("IcmpSendEcho")
	icmp6SendEcho2  = iphlpapi.NewProc("Icmp6SendEcho2")
)

type socketAddressIPv6 struct {
	Family   uint16
	Port     uint16
	FlowInfo uint32
	Address  [16]byte
	ScopeID  uint32
}

type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	TTL           byte
	TOS           byte
	Flags         byte
	OptionsSize   byte
	OptionsData   uintptr
}

type icmp6EchoReply struct {
	Address       socketAddressIPv6
	Status        uint32
	RoundTripTime uint32
}

func pingOnce(ctx context.Context, target net.IPAddr, timeout time.Duration, sequence int) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout <= 0 {
		return 0, context.DeadlineExceeded
	}
	// DWORD milliseconds; round upwards so a sub-millisecond remainder never
	// turns into a zero timeout. Avoid truncation on 32-bit Windows.
	milliseconds := timeout / time.Millisecond
	if timeout%time.Millisecond != 0 {
		milliseconds++
	}
	timeout = min(milliseconds, time.Duration(0xfffffffe)) * time.Millisecond
	if ipv4 := target.IP.To4(); ipv4 != nil {
		return pingIPv4(ipv4, timeout, sequence)
	}
	return pingIPv6(target, timeout, sequence)
}

func pingIPv4(ip net.IP, timeout time.Duration, sequence int) (time.Duration, error) {
	handle, _, callErr := icmpCreateFile.Call()
	if handle == ^uintptr(0) {
		return 0, fmt.Errorf("IcmpCreateFile: %w", normalizeWindowsError(callErr))
	}
	defer icmpCloseHandle.Call(handle)
	payload := makePingPayload(sequence)
	buffer := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(payload)+8)
	destination := binary.LittleEndian.Uint32(ip)
	start := time.Now()
	count, _, callErr := icmpSendEcho.Call(
		handle,
		uintptr(destination),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(timeout.Milliseconds()),
	)
	rtt := time.Since(start)
	if count == 0 {
		return 0, fmt.Errorf("IcmpSendEcho: %w", normalizeWindowsError(callErr))
	}
	reply := (*icmpEchoReply)(unsafe.Pointer(&buffer[0]))
	if reply.Status != 0 {
		return 0, fmt.Errorf("IcmpSendEcho status %d", reply.Status)
	}
	return rtt, nil
}

func pingIPv6(target net.IPAddr, timeout time.Duration, sequence int) (time.Duration, error) {
	handle, _, callErr := icmp6CreateFile.Call()
	if handle == ^uintptr(0) {
		return 0, fmt.Errorf("Icmp6CreateFile: %w", normalizeWindowsError(callErr))
	}
	defer icmpCloseHandle.Call(handle)
	destination := socketAddressIPv6{Family: syscall.AF_INET6}
	copy(destination.Address[:], target.IP.To16())
	if target.Zone != "" {
		iface, err := net.InterfaceByName(target.Zone)
		if err != nil {
			return 0, fmt.Errorf("resolve IPv6 zone: %w", err)
		}
		destination.ScopeID = uint32(iface.Index)
	}
	payload := makePingPayload(sequence)
	buffer := make([]byte, int(unsafe.Sizeof(icmp6EchoReply{}))+len(payload)+8)
	source := socketAddressIPv6{Family: syscall.AF_INET6}
	start := time.Now()
	count, _, callErr := icmp6SendEcho2.Call(
		handle,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&source)),
		uintptr(unsafe.Pointer(&destination)),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(len(payload)),
		0,
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(timeout.Milliseconds()),
	)
	rtt := time.Since(start)
	if count == 0 {
		return 0, fmt.Errorf("Icmp6SendEcho2: %w", normalizeWindowsError(callErr))
	}
	reply := (*icmp6EchoReply)(unsafe.Pointer(&buffer[0]))
	if reply.Status != 0 {
		return 0, fmt.Errorf("Icmp6SendEcho2 status %d", reply.Status)
	}
	return rtt, nil
}

func makePingPayload(sequence int) []byte {
	payload := make([]byte, 16)
	binary.LittleEndian.PutUint64(payload[0:8], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(payload[8:16], uint64(sequence))
	return payload
}

func normalizeWindowsError(err error) error {
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return errors.New("Windows ICMP API call failed")
	}
	return err
}
