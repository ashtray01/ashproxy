package engine

import (
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/windows"
	"net/netip"
	"syscall"
	"unsafe"
)

type Client struct {
	Address, Protocol string
	Port              int
}

var tcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

// MIB_TCPTABLE_OWNER_PID contains a count followed by 24-byte IPv4 rows.
func endpoints(pid int, c Config) (bool, []Client, error) {
	var size uint32 = 4096
	for attempt := 0; attempt < 5; attempt++ {
		buf := make([]byte, size)
		result, _, _ := tcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, 2, 5, 0)
		if result == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if result != 0 {
			return false, nil, syscall.Errno(result)
		}
		count := int(binary.LittleEndian.Uint32(buf))
		if count > (len(buf)-4)/24 {
			return false, nil, fmt.Errorf("invalid TCP table size")
		}
		httpReady, socksReady := c.HTTP == 0, c.SOCKS == 0
		clients := []Client{}
		for i := 0; i < count; i++ {
			r := buf[4+i*24:]
			if int(binary.LittleEndian.Uint32(r[20:])) != pid || netip.AddrFrom4([4]byte(r[4:8])).String() != c.Bind {
				continue
			}
			port := int(binary.BigEndian.Uint16(r[8:10]))
			proto := "HTTP"
			if port == c.SOCKS {
				proto = "SOCKS5"
			} else if port != c.HTTP {
				continue
			}
			switch binary.LittleEndian.Uint32(r) {
			case 2:
				if port == c.HTTP {
					httpReady = true
				} else {
					socksReady = true
				}
			case 5:
				clients = append(clients, Client{netip.AddrFrom4([4]byte(r[12:16])).String(), proto, int(binary.BigEndian.Uint16(r[16:18]))})
			}
		}
		return httpReady && socksReady, clients, nil
	}
	return false, nil, fmt.Errorf("TCP table changed during read")
}
