package addrmgr

import (
	"sync"
	"github.com/georgefff/btcgo/wire"
	"sync/atomic"
	"log"
	"net"
	"errors"
)

type AddrManager struct {
	// The following variables must only be used atomically.
	started  int32
	shutdown int32

	addrNew []*wire.NetAddress
	wg      sync.WaitGroup
	quit    chan struct{}
}

func (am *AddrManager) AddAddresses(addrs []*wire.NetAddress) {
	am.addrNew = append(am.addrNew, addrs...)
}
func (am *AddrManager) GetAddresses() (net.Addr, error) {
	if len(am.addrNew) == 0 {
		return nil, errors.New("addr has no item")
	}
	ip := am.addrNew[0].IP
	am.addrNew = am.addrNew[1:]
	return &net.TCPAddr{
		IP:   ip,
		Port: wire.DefaultPortInt,
	}, nil
}

//AddrHandler is used to handle peer address,maybe read addr form a local file,
//and do something when getaddrMsg and addrMsg interactive.
func (am *AddrManager) addressHandler() {
out:
	for {
		select {
		case <-am.quit:
			break out
		}
	}
	am.wg.Done()
	log.Printf("ADDR:Address handler done")
}
func (am *AddrManager) Start() {
	// Already started?
	if atomic.AddInt32(&am.started, 1) != 1 {
		return
	}
	log.Printf("ADDR:Start address manager")
	am.wg.Add(1)

	go am.addressHandler()
}
func (am *AddrManager) Stop() {
	if atomic.AddInt32(&am.shutdown, 1) != 1 {
		log.Printf("ADDR:Address manager is already in the process of shutting down")
		return
	}

	log.Printf("ADDR:Address manager shutting down")
	// Signal the remaining goroutines to quit.
	close(am.quit)
	am.wg.Wait()
}

func New() *AddrManager {
	am := AddrManager{
		quit: make(chan struct{}),
	}
	return &am
}
