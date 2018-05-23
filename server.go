package main

import (
	"sync"
	"github.com/georgefff/btcgo/addrmgr"
	"github.com/georgefff/btcgo/connmgr"
	"github.com/georgefff/btcgo/syncmgr"
	"sync/atomic"
	"log"
	"github.com/georgefff/btcgo/wire"
	"github.com/georgefff/btcgo/config"
	"net"
	"github.com/georgefff/btcgo/peer"
)

// DefaultServices describes the default services that are supported by the server.
const defaultServices = wire.SFNodeNetwork | wire.SFNodeBloom | wire.SFNodeWitness

type server struct {
	// The following variables must only be used atomically.
	started  int32
	shutdown int32

	addrManager *addrmgr.AddrManager
	connManager *connmgr.ConnManager
	syncManager *syncmgr.SyncManager
	services    wire.ServiceFlag
	newPeers    chan *serverPeer
	donePeers   chan *serverPeer

	wg   sync.WaitGroup
	quit chan struct{}
}

// serverPeer extends the peer to maintain state shared by the server and the blockmanager.
type serverPeer struct {
	*peer.Peer

	connReq *connmgr.ConnReq
	server  *server
	quit    chan struct{}
}
type peerState struct {
	outboundPeers map[int32]*serverPeer
}

// OnVersion is invoked when a peer receives a version bitcoin message
func (sp *serverPeer) OnVersion(_ *peer.Peer, msg *wire.MsgVersion) {
	// Signal the sync manager this peer is a new sync candidate.
	sp.server.syncManager.NewPeer(sp.Peer)
	// Outbound connections.
	if !sp.Inbound() {
		log.Printf("ONVERSION:We can push addr/getAddr msg at there")
	}
	// Add valid peer to the server.
	sp.server.AddPeer(sp)
}
func (sp *serverPeer) OnBlock(_ *peer.Peer, msg *wire.MsgBlock, buf []byte) {

	sp.server.syncManager.QueueBlock(msg, sp.Peer)
}
func (sp *serverPeer) OnHeaders(_ *peer.Peer, msg *wire.MsgHeaders) {
	sp.server.syncManager.QueueHeaders(msg, sp.Peer)
}
func (sp *serverPeer) OnGetHeaders(_ *peer.Peer, msg *wire.MsgGetHeaders) {

}

// outboundPeerConnected is invoked when connManager connected a outboundPeer
func (s *server) outboundPeerConnected(connReq *connmgr.ConnReq, conn net.Conn) {
	serverPeer := newServerPeer(s)
	p, err := peer.NewOutboundPeer(newPeerConfig(serverPeer), connReq.Addr.String())
	if err != nil {
		log.Printf("SERVER:Cannot create outbound peer %s: %v", connReq.Addr, err)
		s.connManager.Disconnect(connReq.ID())
	}
	serverPeer.Peer = p
	serverPeer.connReq = connReq
	serverPeer.AssociateConnection(conn)
	go s.peerDoneHandler(serverPeer)
}

func (s *server) handleAddPeerMsg(state *peerState, serverPeer *serverPeer) {
	if serverPeer == nil {
		return
	}
	// Ignore new peers if we're shutting down.
	if atomic.LoadInt32(&s.shutdown) != 0 {
		log.Printf("SERVER:New peer %s ignored - server is shutting down", serverPeer)
		serverPeer.Disconnect()
	}
	// Limit max number of total peers.
	if len(state.outboundPeers) >= config.DefaultMaxPeers {
		log.Printf("SERVER:Max peers reached [%d] - disconnecting peer %s", config.DefaultMaxPeers, serverPeer)
		serverPeer.Disconnect()
	}

	// Add the new peer and start it.
	log.Printf("SERVER:New peer %s", serverPeer)
	state.outboundPeers[serverPeer.ID()] = serverPeer

}
func (s *server) handleDonePeerMsg(state *peerState, serverPeer *serverPeer) {
	list := state.outboundPeers
	if _, ok := list[serverPeer.ID()]; ok {
		if !serverPeer.Inbound() && serverPeer.connReq != nil {
			s.connManager.Disconnect(serverPeer.connReq.ID())
		}
		delete(list, serverPeer.ID())
		log.Printf("SERVER:Removed peer %s", serverPeer)
		return
	}

	if serverPeer.connReq != nil {
		s.connManager.Disconnect(serverPeer.connReq.ID())
	}
}

// peerHandler is used to handle peer operations such as adding and removing peers to and from the server,
// banning peers, and broadcasting messages to peers.  It must be run in a goroutine.
func (s *server) peerHandler() {
	s.addrManager.Start()
	s.syncManager.Start()

	log.Printf("SERVER:Start peer handler")

	state := &peerState{
		outboundPeers: make(map[int32]*serverPeer),
	}

	// Add peers discovered through DNS to the address manager.
	connmgr.SeedFromDNS(func(addrs []*wire.NetAddress) {
		s.addrManager.AddAddresses(addrs)
	})

	go s.connManager.Start()

out:
	for {
		select {
		// New peers connected to the server.
		case p := <-s.newPeers:
			s.handleAddPeerMsg(state, p)

			// Disconnected peers.
		case p := <-s.donePeers:
			s.handleDonePeerMsg(state, p)

		case <-s.quit:
			// Disconnect all peers on server shutdown.
			for _, sp := range state.outboundPeers {
				log.Printf("SERVER:Shutdown peer %s", sp)
				sp.Disconnect()
			}
			break out
		}
	}

	s.connManager.Stop()
	s.syncManager.Stop()
	s.addrManager.Stop()

	// Drain channels before exiting so nothing is left waiting around to send.
cleanup:
	for {
		select {
		case <-s.newPeers:
		case <-s.donePeers:
		default:
			break cleanup
		}
	}
	s.wg.Done()
	log.Printf("SERVER:Peer handler done")
}
func (s *server) AddPeer(sp *serverPeer) {
	s.newPeers <- sp
}
func (s *server) peerDoneHandler(sp *serverPeer) {
	sp.WaitForDisconnect()
	s.donePeers <- sp

	// Only tell sync manager we are gone if we ever told it we existed.
	if sp.VersionKnown() {
		s.syncManager.DonePeer(sp.Peer)
	}
	close(sp.quit)
}

func (s *server) start() {
	// Already started?
	if atomic.AddInt32(&s.started, 1) != 1 {
		return
	}
	log.Printf("SERVER:Start server")
	s.wg.Add(1)

	go s.peerHandler()
}
func (s *server) Stop() {
	// Make sure this only happens once.
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		log.Printf("SERVER:Server is already in the process of shutting down")
		return
	}

	// Signal the remaining goroutines to quit.
	close(s.quit)
	log.Printf("SERVER:Server shutting down")
}
func (s *server) WaitForShutdown() {
	s.wg.Wait()
}

func newPeerConfig(sp *serverPeer) *peer.Config {
	return &peer.Config{
		Listeners: peer.MessageListeners{
			OnVersion:    sp.OnVersion,
			OnBlock:      sp.OnBlock,
			OnHeaders:    sp.OnHeaders,
			OnGetHeaders: sp.OnGetHeaders,
		},
		ProtocolVersion: wire.ProtocolVersion,
		Services:        sp.server.services,
		DisableRelayTx:  false,
	}
}
func newServerPeer(s *server) *serverPeer {
	return &serverPeer{
		server: s,
		quit:   make(chan struct{}),
	}
}
func NewServer() *server {

	s := server{
		services:  defaultServices,
		newPeers:  make(chan *serverPeer),
		donePeers: make(chan *serverPeer),
		quit:      make(chan struct{}),
	}
	s.addrManager = addrmgr.New()

	s.connManager = connmgr.New(&connmgr.Config{
		RetryDuration:  config.ConnectionRetryInterval,
		TargetOutbound: config.DefaultTargetOutbound,
		OnConnection:   s.outboundPeerConnected,
		GetNewAddress:  s.addrManager.GetAddresses,
		Dial:           config.BtcdDial,
	})
	s.syncManager = syncmgr.New()

	return &s
}
