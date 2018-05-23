package connmgr

import (
	"sync"
	"log"

	"sync/atomic"
	"net"
	"time"
	"github.com/georgefff/btcgo/config"
	"fmt"
)

type ConnReq struct {
	// The following variables must only be used atomically.
	id uint64

	Addr net.Addr
	conn net.Conn
}

// connectedReq is used to queue a successful connection.
type connected struct {
	c    *ConnReq
	conn net.Conn
}

// disconnectedReq is used to remove a connection.
type disconnected struct {
	id uint64
}

// failedReq is used to remove a pending connection.
type failed struct {
	c   *ConnReq
	err error
}

type Config struct {
	// TargetOutbound is the number of outbound network connections to maintain. Defaults to 8.
	TargetOutbound uint32

	// RetryDuration is the duration to wait before retrying connection requests. Defaults to 5s.
	RetryDuration time.Duration

	// OnConnection is a callback that is fired when a new outbound connection is established.
	OnConnection func(*ConnReq, net.Conn)

	// GetNewAddress is a way to get an address to make a network connection to.
	// If nil, no new connections will be made automatically.
	GetNewAddress func() (net.Addr, error)

	// Dial connects to the address on the named network. It cannot be nil.
	Dial func(net.Addr) (net.Conn, error)
}
type ConnManager struct {
	// The following variables must only be used atomically.
	started      int32
	shutdown     int32
	connReqCount uint64

	//received requests
	reqChan chan interface{}
	cfg     Config

	wg   sync.WaitGroup
	quit chan struct{}
}

func (cm *ConnReq) ID() uint64 {
	return atomic.LoadUint64(&cm.id)
}

func (cm *ConnManager) handleFailedConn(req *ConnReq) {
	//Maybe do more other thing, we just reconnect a new at there
	go cm.newConnReq()
}

//ConnHandler is used to handle peer connection from addrManager.
func (cm *ConnManager) connHandler() {
	connMap := make(map[uint64]*ConnReq, cm.cfg.TargetOutbound)
out:
	for {
		select {
		case request := <-cm.reqChan:
			switch req := request.(type) {

			case connected:
				connReq := req.c
				connReq.conn = req.conn
				connMap[connReq.id] = connReq
				log.Printf("CONN:Connected to %v", connReq)

				if cm.cfg.OnConnection != nil {
					go cm.cfg.OnConnection(connReq, req.conn)
				}

			case disconnected:
				if connReq, ok := connMap[req.id]; ok {
					if connReq.conn != nil {
						connReq.conn.Close()
					}
					log.Printf("CONN:Disconnected from %v", connReq)
					delete(connMap, req.id)

					if uint32(len(connMap)) < cm.cfg.TargetOutbound {
						go cm.newConnReq()
					}
				} else {
					log.Printf("CONN:Unknown connection: %d", req.id)
				}

			case failed:
				connReq := req.c
				log.Printf("CONN:Failed to connect to %v: %v", connReq, req.err)
				cm.handleFailedConn(connReq)
			}

		case <-cm.quit:
			break out
		}
	}

	cm.wg.Done()
	log.Printf("CONN:Connection handler done")
}
func (cm *ConnManager) newConnReq() {
	if atomic.LoadInt32(&cm.shutdown) != 0 {
		return
	}
	if cm.cfg.GetNewAddress == nil {
		return
	}

	connReq := &ConnReq{}
	//Assigns an id
	atomic.StoreUint64(&connReq.id, atomic.AddUint64(&cm.connReqCount, 1))

	addr, err := cm.cfg.GetNewAddress()
	if err != nil {
		cm.reqChan <- failed{connReq, err}
		return
	}
	connReq.Addr = addr

	cm.connect(connReq)
}
func (cm *ConnManager) connect(connReq *ConnReq) {
	if atomic.LoadInt32(&cm.shutdown) != 0 {
		return
	}
	if atomic.LoadUint64(&connReq.id) == 0 {
		atomic.StoreUint64(&connReq.id, atomic.AddUint64(&cm.connReqCount, 1))
	}
	log.Printf("CONN:Attempting to connect to %v", connReq)

	conn, err := cm.cfg.Dial(connReq.Addr)
	if err != nil {
		cm.reqChan <- failed{connReq, err}
	} else {
		cm.reqChan <- connected{connReq, conn}
	}

}
func (cm *ConnManager) Disconnect(id uint64) {
	if atomic.LoadInt32(&cm.shutdown) != 0 {
		return
	}
	cm.reqChan <- disconnected{id}
}
func (cm *ConnManager) Start() {
	// Already started?
	if atomic.AddInt32(&cm.started, 1) != 1 {
		return
	}
	log.Printf("CONN:Start connection manager")
	cm.wg.Add(1)
	go cm.connHandler()

	for i := atomic.LoadUint64(&cm.connReqCount); i < uint64(cm.cfg.TargetOutbound); i++ {
		go cm.newConnReq()
	}
}
func (cm *ConnManager) Stop() {
	if atomic.AddInt32(&cm.shutdown, 1) != 1 {
		log.Printf("CONN:Connection manager is already in the process of shutting down")
		return
	}
	close(cm.quit)
	cm.wg.Wait()
	fmt.Println("CONN:Connection manager shutting down")
}

func New(cfg *Config) *ConnManager {
	if cfg.Dial == nil {
		return nil
	}
	if cfg.RetryDuration <= 0 {
		cfg.RetryDuration = config.ConnectionRetryInterval
	}
	if cfg.TargetOutbound == 0 {
		cfg.TargetOutbound = config.DefaultTargetOutbound
	}

	cm := ConnManager{
		cfg:     *cfg, // Copy so caller can't mutate
		reqChan: make(chan interface{}),
		quit:    make(chan struct{}),
	}
	return &cm
}
