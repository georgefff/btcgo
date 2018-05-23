package syncmgr

import (
	"sync"
	"github.com/georgefff/btcgo/peer"
	"sync/atomic"
	"log"
	"github.com/georgefff/btcgo/wire"
	"github.com/georgefff/btcgo/config"
	"fmt"
	"github.com/georgefff/btcgo/blockchain"
	"github.com/georgefff/btcgo/blockchain/utils"
	"bytes"
)

type newPeerMsg struct {
	peer *peer.Peer
}
type blockMsg struct {
	block *wire.MsgBlock
	peer  *peer.Peer
}
type headersMsg struct {
	headers *wire.MsgHeaders
	peer    *peer.Peer
}
type donePeerMsg struct {
	peer *peer.Peer
}
type SyncManager struct {
	// The following variables must only be used atomically.
	started  int32
	shutdown int32

	syncPeer  *peer.Peer
	hashChain []*utils.Hash
	msgChan   chan interface{}

	wg   sync.WaitGroup
	quit chan struct{}
}

func (sm *SyncManager) handleBlockMsg(bmsg *blockMsg) {
	// handleBlockMsg handles block messages from all peers.
	log.Printf("SYNC:handleBlockMsg receive block : %+v", bmsg.block)
	for i, tx := range bmsg.block.Transactions {
		log.Printf("SYNC:handleBlockMsg receive transaction%d : %+v", i, *tx)
	}
}
func (sm *SyncManager) fetchHeaderBlocks() {
	gdmsg := wire.NewMsgGetData()
	iv := wire.NewInvVect(wire.InvTypeBlock, sm.hashChain[1])
	gdmsg.AddInvVect(iv)
	if len(gdmsg.InvList) > 0 {
		sm.syncPeer.QueueMessage(gdmsg, nil)
	}
}
func (sm *SyncManager) handleHeadersMsg(hmsg *headersMsg) {
	log.Printf("SYNC:HandleHeadersMsg len(*headersMsg) is %d", len(hmsg.headers.Headers))

	for _, header := range hmsg.headers.Headers {
		sm.hashChain = append(sm.hashChain, &header.PrevBlock)
	}
	sm.hashChain = sm.hashChain[1:]

	//just verify first block hash
	hFirst := hmsg.headers.Headers[0]
	var hbuf bytes.Buffer
	if err := hFirst.Serialize(&hbuf); err != nil {
		log.Printf("SYNC:HandleHeadersMsg serialize header err : %v", err)
	}
	if hash := utils.DoubleHashH(hbuf.Bytes()); hash == *sm.hashChain[1] {
		log.Printf("SYNC:HandleHeadersMsg verify a block header success")
	} else {
		log.Printf("SYNC:HandleHeadersMsg verify a block header failed,"+
			"verify is %s,but actually is %s", hash, *sm.hashChain[1])
	}

	sm.fetchHeaderBlocks()
}
func (sm *SyncManager) handleNewPeerMsg(peer *peer.Peer) {
	// Ignore if in the process of shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}
	log.Printf("SYNC:New sync peer %s (%s)\n", peer, peer.UserAgent())

	sm.syncPeer = peer
	var locator blockchain.BlockLocator
	locator = append(locator, sm.hashChain[0])
	peer.PushGetHeadersMsg(locator, &utils.Hash{})
}

//syncHandler is used to handle block headers and blocks sync.
func (sm *SyncManager) blockHandler() {
out:
	for {
		select {
		case m := <-sm.msgChan:
			switch msg := m.(type) {
			case *newPeerMsg:
				sm.handleNewPeerMsg(msg.peer)
			case *headersMsg:
				sm.handleHeadersMsg(msg)
			case *blockMsg:
				sm.handleBlockMsg(msg)
			case *donePeerMsg:
				//sm.handleDonePeerMsg(msg.peer)

			default:
				fmt.Printf("Invalid message type in block handler: %T\n", msg)
			}
		case <-sm.quit:
			break out
		}
	}
	sm.wg.Done()
	fmt.Println("Block handler done")
}
func (sm *SyncManager) QueueBlock(block *wire.MsgBlock, peer *peer.Peer) {
	// Don't accept more blocks if we're shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &blockMsg{block: block, peer: peer}
}
func (sm *SyncManager) QueueHeaders(headers *wire.MsgHeaders, peer *peer.Peer) {
	// No channel handling here because peers do not need to block on
	// headers messages.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &headersMsg{headers: headers, peer: peer}
}
func (sm *SyncManager) NewPeer(peer *peer.Peer) {
	// Ignore if we are shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}
	sm.msgChan <- &newPeerMsg{peer: peer}
}
func (sm *SyncManager) DonePeer(p *peer.Peer) {
	// Ignore if we are shutting down.
	if atomic.LoadInt32(&sm.shutdown) != 0 {
		return
	}

	sm.msgChan <- &donePeerMsg{peer: p}
}
func (sm *SyncManager) Start() {
	// Already started?
	if atomic.AddInt32(&sm.started, 1) != 1 {
		return
	}
	log.Printf("SYNC:Start sync manager")
	sm.wg.Add(1)
	go sm.blockHandler()
}
func (sm *SyncManager) Stop() {
	if atomic.AddInt32(&sm.shutdown, 1) != 1 {
		log.Println("Sync manager is already in the process of shutting down")
		return
	}
	close(sm.quit)
	sm.wg.Wait()
	log.Println("SYNC:Sync manager shutting down")
}

func New() *SyncManager {

	sm := SyncManager{
		msgChan: make(chan interface{}, config.DefaultMaxPeers*3),
		quit:    make(chan struct{}),
	}
	sm.hashChain = append(sm.hashChain, &blockchain.GenesisHash)
	return &sm
}
