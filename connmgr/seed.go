package connmgr

import (
	"github.com/georgefff/btcgo/wire"
	"time"
	"net"
	"log"
	mrand "math/rand"
)

const (
	// These constants are used by the DNS seed code to pick a random last seen time.
	secondsIn3Days int32 = 24 * 60 * 60 * 3
	secondsIn4Days int32 = 24 * 60 * 60 * 4
)

var DNSSeeds = []string{
	"seed.bitcoin.sipa.be",
	"dnsseed.bluematt.me",
	"seed.bitcoinstats.com",
	"seed.bitnodes.io",
	"seed.bitcoin.jonasschnelli.ch",
}

func SeedFromDNS(seedFn func(addrs []*wire.NetAddress)) {
	resChan := make(chan bool)
	for _, host := range DNSSeeds {
		go func(host string) {
			randSource := mrand.New(mrand.NewSource(time.Now().UnixNano()))
			peers, err := net.LookupIP(host)
			if err != nil {
				log.Printf("CONN:DNS discovery failed on seed %s: %v", host, err)
				resChan <- false
				return
			}

			num := len(peers)
			log.Printf("CONN:DNS discovery %d addresses from seed %s\n", num, host)
			if num == 0 {
				resChan <- false
				return
			}

			addrs := make([]*wire.NetAddress, num)
			intPort := wire.DefaultPortInt
			for i, ip := range peers {
				addrs[i] = wire.NewNetAddressTimestamp(
					// bitcoind seeds with addresses from a time randomly selected between 3 and 7 days ago.
					time.Now().Add(-1*time.Second*time.Duration(secondsIn3Days+randSource.Int31n(secondsIn4Days))),
					0, ip, uint16(intPort))
			}
			seedFn(addrs)
			resChan <- true
		}(host)
		<-resChan
	}
}
