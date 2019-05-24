package ebpf

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/DataDog/datadog-agent/pkg/ebpf/netlink"
	"github.com/DataDog/datadog-agent/pkg/process/util"
)

// ConnectionType will be either TCP or UDP
type ConnectionType uint8

const (
	// TCP connection type
	TCP ConnectionType = 0

	// UDP connection type
	UDP ConnectionType = 1
)

func (c ConnectionType) String() string {
	if c == TCP {
		return "TCP"
	}
	return "UDP"
}

// ConnectionFamily will be either v4 or v6
type ConnectionFamily uint8

// ConnectionDirection indicates if the connection is incoming to the host or outbound
type ConnectionDirection uint8

const (
	// INCOMING represents connections inbound to the host
	INCOMING ConnectionDirection = 1

	// OUTGOING represents outbound connections from the host
	OUTGOING ConnectionDirection = 2

	// LOCAL represents connections that don't leave the host
	LOCAL ConnectionDirection = 3
)

func (d ConnectionDirection) String() string {
	switch d {
	case OUTGOING:
		return "outgoing"
	case LOCAL:
		return "local"
	default:
		return "incoming"
	}
}

const (
	// AFINET represents v4 connections
	AFINET ConnectionFamily = 0

	// AFINET6 represents v6 connections
	AFINET6 ConnectionFamily = 1
)

// Connections wraps a collection of ConnectionStats
type Connections struct {
	Conns []ConnectionStats
}

// ConnectionStats stores statistics for a single connection.  Field order in the struct should be 8-byte aligned
type ConnectionStats struct {
	// Source & Dest represented as a string to handle both IPv4 & IPv6
	Source util.Address
	Dest   util.Address

	MonotonicSentBytes uint64
	LastSentBytes      uint64

	MonotonicRecvBytes uint64
	LastRecvBytes      uint64

	// Last time the stats for this connection were updated
	LastUpdateEpoch uint64

	MonotonicRetransmits uint32
	LastRetransmits      uint32

	Pid   uint32
	NetNS uint32

	SPort         uint16
	DPort         uint16
	Type          ConnectionType
	Family        ConnectionFamily
	Direction     ConnectionDirection
	IPTranslation *netlink.IPTranslation
}

func (c ConnectionStats) String() string {
	return fmt.Sprintf(
		"[%s] [PID: %d] [%v:%d ⇄ %v:%d] (%s) %d bytes sent (+%d), %d bytes received (+%d), %d retransmits (+%d)",
		c.Type,
		c.Pid,
		c.Source,
		c.SPort,
		c.Dest,
		c.DPort,
		c.Direction,
		c.MonotonicSentBytes, c.LastSentBytes,
		c.MonotonicRecvBytes, c.LastRecvBytes,
		c.MonotonicRetransmits, c.LastRetransmits,
	)
}

// ByteKey returns a unique key for this connection represented as a byte array
func (c ConnectionStats) ByteKey(buffer *bytes.Buffer) ([]byte, error) {
	buffer.Reset()
	// Byte-packing to improve creation speed
	// PID (32 bits) + SPort (16 bits) + DPort (16 bits) = 64 bits
	p0 := uint64(c.Pid)<<32 | uint64(c.SPort)<<16 | uint64(c.DPort)

	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], p0)

	if _, err := buffer.Write(buf[:]); err != nil {
		return nil, err
	}
	if _, err := buffer.Write(c.Source.Bytes()); err != nil {
		return nil, err
	}
	buffer.WriteRune('|')
	// Family (4 bits) + Type (4 bits) = 8 bits
	p1 := uint8(c.Family)<<4 | uint8(c.Type)
	if err := buffer.WriteByte(p1); err != nil {
		return nil, err
	}
	buffer.WriteRune('|')
	if _, err := buffer.Write(c.Dest.Bytes()); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

const keyFmt = "p:%d|src:%s:%d|dst:%s:%d|f:%d|t:%d"

// BeautifyKey returns a human readable byte key (used for debugging purposes)
// it should be in sync with ByteKey
// Note: This is only used in /debug/* endpoints
func BeautifyKey(key string) string {
	bytesToAddress := func(buf []byte) util.Address {
		if len(buf) == 4 {
			return util.V4AddressFromBytes(buf)
		}
		return util.V6AddressFromBytes(buf)
	}

	raw := []byte(key)

	// First 8 bytes are pid and ports
	h := binary.LittleEndian.Uint64(raw[:8])
	pid := h >> 32
	sport := (h >> 16) & 0xffff
	dport := h & 0xffff

	// Them we have the source addr, family + type and dest addr
	parts := bytes.Split(raw[8:], []byte{'|'})

	var source, dest util.Address
	var family, typ uint8
	if len(parts) == 3 {
		source = bytesToAddress(parts[0])
		dest = bytesToAddress(parts[2])
		if len(parts[1]) == 1 {
			family = (parts[1][0] >> 4) & 0xf
			typ = parts[1][0] & 0xf
		}
	}

	return fmt.Sprintf(keyFmt, pid, source, sport, dest, dport, family, typ)
}
