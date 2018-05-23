package config

import (
	"time"
	"net"
)

const (
	ConnectionRetryInterval = time.Second * 5
	DefaultTargetOutbound   = uint32(1)
	DefaultMaxPeers         = 125
)
const (
	defaultConnectTimeout = time.Second * 5
	DefaultUserAgent      = "/btcgo:0.5.0/"
)

func BtcdDial(addr net.Addr) (net.Conn, error) {
	//We also can dial onion
	return net.DialTimeout(addr.Network(), addr.String(), defaultConnectTimeout)
}
