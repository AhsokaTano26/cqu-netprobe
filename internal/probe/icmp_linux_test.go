//go:build linux

package probe

import (
	"encoding/binary"
	"net"
	"syscall"
	"testing"
)

func TestReplyValidation(t *testing.T) {
	request := makeEchoRequest(icmpEchoRequest, 123, 42, 98765)
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
		valid  bool
	}{
		{"valid", func([]byte) {}, true},
		{"wrong code", func(p []byte) { p[1] = 1 }, false},
		{"wrong id", func(p []byte) { p[4]++ }, false},
		{"wrong sequence", func(p []byte) { p[6]++ }, false},
		{"wrong payload", func(p []byte) { p[8]++ }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply := append([]byte(nil), request...)
			reply[0] = icmpEchoReply
			tc.mutate(reply)
			reply[2], reply[3] = 0, 0
			binary.BigEndian.PutUint16(reply[2:4], checksum(reply))
			if matchesEchoReply(reply, request, icmpEchoReply, true) != tc.valid {
				t.Fatal("unexpected reply acceptance")
			}
		})
	}
	if samePeer(&syscall.SockaddrInet4{Addr: [4]byte{127, 0, 0, 2}}, net.ParseIP("127.0.0.1")) {
		t.Fatal("accepted wrong source")
	}
}
