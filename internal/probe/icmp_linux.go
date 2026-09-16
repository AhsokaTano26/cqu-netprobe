//go:build linux

package probe

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	icmpEchoReply    = 0
	icmpEchoRequest  = 8
	icmp6EchoRequest = 128
	icmp6EchoReply   = 129
)

var echoToken atomic.Uint64

func pingOnce(ctx context.Context, target net.IPAddr, timeout time.Duration, sequence int) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	ipv4 := target.IP.To4()
	family, protocol, requestType, replyType := syscall.AF_INET6, syscall.IPPROTO_ICMPV6, byte(icmp6EchoRequest), byte(icmp6EchoReply)
	if ipv4 != nil {
		family, protocol, requestType, replyType = syscall.AF_INET, syscall.IPPROTO_ICMP, icmpEchoRequest, icmpEchoReply
	}

	fd, privileged, err := openICMPSocket(family, protocol)
	if err != nil {
		return 0, err
	}
	defer syscall.Close(fd)
	timeval := syscall.NsecToTimeval(timeout.Nanoseconds())
	if err := syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &timeval); err != nil {
		return 0, fmt.Errorf("set ICMP receive timeout: %w", err)
	}

	token := uint64(time.Now().UnixNano()) ^ echoToken.Add(1)
	identifier := uint16(os.Getpid()) ^ uint16(token)
	packet := makeEchoRequest(requestType, identifier, uint16(sequence), token)
	var destination syscall.Sockaddr
	if ipv4 != nil {
		address := &syscall.SockaddrInet4{}
		copy(address.Addr[:], ipv4)
		destination = address
	} else {
		address := &syscall.SockaddrInet6{}
		copy(address.Addr[:], target.IP.To16())
		if target.Zone != "" {
			iface, lookupErr := net.InterfaceByName(target.Zone)
			if lookupErr != nil {
				return 0, fmt.Errorf("resolve IPv6 zone: %w", lookupErr)
			}
			address.ZoneId = uint32(iface.Index)
		}
		destination = address
	}

	start := time.Now()
	if err := syscall.Sendto(fd, packet, 0, destination); err != nil {
		return 0, fmt.Errorf("send ICMP echo: %w", err)
	}
	buffer := make([]byte, 1500)
	deadline := start.Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		count, _, err := syscall.Recvfrom(fd, buffer, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) && time.Now().Before(deadline) {
				continue
			}
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				return 0, errors.New("ICMP echo timed out")
			}
			return 0, fmt.Errorf("receive ICMP echo: %w", err)
		}
		reply := buffer[:count]
		if ipv4 != nil && privileged && len(reply) >= 20 && reply[0]>>4 == 4 {
			headerLength := int(reply[0]&0x0f) * 4
			if headerLength > len(reply) {
				continue
			}
			reply = reply[headerLength:]
		}
		if len(reply) < 16 || reply[0] != replyType || binary.BigEndian.Uint16(reply[6:8]) != uint16(sequence) || binary.BigEndian.Uint64(reply[8:16]) != token {
			continue
		}
		return time.Since(start), nil
	}
}

func openICMPSocket(family, protocol int) (fd int, privileged bool, err error) {
	fd, err = syscall.Socket(family, syscall.SOCK_DGRAM, protocol)
	if err == nil {
		return fd, false, nil
	}
	fd, rawErr := syscall.Socket(family, syscall.SOCK_RAW, protocol)
	if rawErr != nil {
		return -1, false, fmt.Errorf("open ICMP socket (unprivileged: %v; raw: %w)", err, rawErr)
	}
	return fd, true, nil
}

func makeEchoRequest(messageType byte, identifier, sequence uint16, token uint64) []byte {
	packet := make([]byte, 16)
	packet[0] = messageType
	binary.BigEndian.PutUint16(packet[4:6], identifier)
	binary.BigEndian.PutUint16(packet[6:8], sequence)
	binary.BigEndian.PutUint64(packet[8:16], token)
	if messageType == icmpEchoRequest {
		binary.BigEndian.PutUint16(packet[2:4], checksum(packet))
	}
	return packet
}

func checksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data[:2]))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	return ^uint16(sum)
}
