package peer

import (
	"net"
	"strconv"
	"github.com/georgefff/btcgo/wire"
	"sync/atomic"
	"log"
	"sync"
	"fmt"
	"time"
	"errors"
	"math/rand"
	"container/list"
	"io"
	"github.com/georgefff/btcgo/blockchain"
	"github.com/georgefff/btcgo/blockchain/utils"
)

const (
	// outputBufferSize is the number of elements the output channels use.
	outputBufferSize = 50
	// negotiateTimeout is the duration of inactivity before we timeout a
	// peer that hasn't completed the initial version negotiation.
	negotiateTimeout = 30 * time.Second
	// minAcceptableProtocolVersion is the lowest protocol version that a
	// connected peer may support.
	minAcceptableProtocolVersion = wire.MultipleAddressVersion
	// pingInterval is the interval of time to wait in between sending ping
	// messages.
	pingInterval = 2 * time.Minute
	// stallTickInterval is the interval of time between each check for
	// stalled peers.
	stallTickInterval = 15 * time.Second
	// stallResponseTimeout is the base maximum amount of time messages that
	// expect a response will wait before disconnecting the peer for
	// stalling.  The deadlines are adjusted for callback running times and
	// only checked on each stall tick interval.
	stallResponseTimeout = 30 * time.Second
	// idleTimeout is the duration of inactivity before we time out a peer.
	idleTimeout = 5 * time.Minute

	defaultServices = wire.SFNodeNetwork | wire.SFNodeBloom | wire.SFNodeWitness
)
const (
	// sccSendMessage indicates a message is being sent to the remote peer.
	sccSendMessage stallControlCmd = iota

	// sccReceiveMessage indicates a message has been received from the
	// remote peer.
	sccReceiveMessage

	// sccHandlerStart indicates a callback handler is about to be invoked.
	sccHandlerStart

	// sccHandlerStart indicates a callback handler has completed.
	sccHandlerDone
)

type outMsg struct {
	msg      wire.Message
	doneChan chan<- struct{}
	encoding wire.MessageEncoding
}
type stallControlCmd uint8
type stallControlMsg struct {
	command stallControlCmd
	message wire.Message
}

type MessageListeners struct {
	// OnBlock is invoked when a peer receives a block bitcoin message.
	OnBlock func(p *Peer, msg *wire.MsgBlock, buf []byte)

	// OnHeaders is invoked when a peer receives a headers bitcoin message.
	OnHeaders func(p *Peer, msg *wire.MsgHeaders)

	// OnGetHeaders is invoked when a peer receives a getheaders bitcoin
	// message.
	OnGetHeaders func(p *Peer, msg *wire.MsgGetHeaders)

	// OnVersion is invoked when a peer receives a version bitcoin message.
	OnVersion func(p *Peer, msg *wire.MsgVersion)
}
type Config struct {
	// UserAgentName specifies the user agent name to advertise.  It is
	// highly recommended to specify this value.
	UserAgentName string

	// Services specifies which services to advertise as supported by the
	// local peer.  This field can be omitted in which case it will be 0
	// and therefore advertise no supported services.
	Services wire.ServiceFlag

	// ProtocolVersion specifies the maximum protocol version to use and
	// advertise.  This field can be omitted in which case
	// peer.MaxProtocolVersion will be used.
	ProtocolVersion uint32

	// DisableRelayTx specifies if the remote peer should be informed to
	// not send inv messages for transactions.
	DisableRelayTx bool

	// Listeners houses callback functions to be invoked on receiving peer
	// messages.
	Listeners MessageListeners
}
type Peer struct {
	// The following variables must only be used atomically.
	connected  int32
	disconnect int32

	conn net.Conn

	// These fields are set at creation time and never modified, so they are
	// safe to read from concurrently without a mutex.
	addr    string
	cfg     Config
	inbound bool

	flagsMtx           sync.Mutex // protects the peer flags below
	na                 *wire.NetAddress
	id                 int32
	userAgent          string
	services           wire.ServiceFlag
	versionKnown       bool
	advertisedProtoVer uint32 // protocol version advertised by remote
	protocolVersion    uint32 // negotiated protocol version

	wireEncoding wire.MessageEncoding

	statsMtx       sync.RWMutex
	lastBlock      int32

	stallControl  chan stallControlMsg
	outputQueue   chan outMsg
	sendQueue     chan outMsg
	sendDoneQueue chan struct{}
	inQuit        chan struct{}
	queueQuit     chan struct{}
	outQuit       chan struct{}
	quit          chan struct{}
}

func (p *Peer) String() string {
	if p.inbound {
		return fmt.Sprintf("%s (%s)", p.addr, "inbound")
	}

	return fmt.Sprintf("%s (%s)", p.addr, "outbound")
}
func minUint32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
func (p *Peer) ID() int32 {
	p.flagsMtx.Lock()
	id := p.id
	p.flagsMtx.Unlock()

	return id
}
func (p *Peer) Inbound() bool {
	return p.inbound
}
func (p *Peer) VersionKnown() bool {
	p.flagsMtx.Lock()
	versionKnown := p.versionKnown
	p.flagsMtx.Unlock()

	return versionKnown
}
func (p *Peer) UserAgent() string {
	p.flagsMtx.Lock()
	userAgent := p.userAgent
	p.flagsMtx.Unlock()
	return userAgent
}

func (p *Peer) handlePeerVersion(msg *wire.MsgVersion) error {

	if uint32(msg.ProtocolVersion) < minAcceptableProtocolVersion {
		reason := fmt.Sprintf("protocol version must be %d or greater", minAcceptableProtocolVersion)
		return errors.New(reason)
	}

	p.statsMtx.Lock()
	p.lastBlock = msg.LastBlock
	p.statsMtx.Unlock()

	// Negotiate the protocol version.
	p.flagsMtx.Lock()
	p.advertisedProtoVer = uint32(msg.ProtocolVersion)
	p.protocolVersion = minUint32(p.protocolVersion, p.advertisedProtoVer)
	p.versionKnown = true
	p.services = msg.Services
	p.userAgent = msg.UserAgent
	p.flagsMtx.Unlock()

	return nil
}
func (p *Peer) writeLocalVersionMsg() error {
	me := &wire.NetAddress{
		Timestamp: time.Unix(time.Now().Unix(), 0),
		Services:  defaultServices,
		IP:        net.ParseIP(""),
		Port:      0,
	}
	localVerMsg := wire.NewMsgVersion(me, p.na, uint64(rand.Int63()), 0)
	return p.writeMessage(localVerMsg, wire.WitnessEncoding)
}
func (p *Peer) readRemoteVersionMsg() error {

	msg, _, err := p.readMessage(wire.WitnessEncoding)
	if err != nil {
		return err
	}
	remoteVerMsg, ok := msg.(*wire.MsgVersion)
	if !ok {
		rejectMsg := wire.NewMsgReject(msg.Command(), wire.RejectMalformed, "Version message must precede all others")
		return p.writeMessage(rejectMsg, wire.WitnessEncoding)
	}
	if err := p.handlePeerVersion(remoteVerMsg); err != nil {
		return err
	}

	if p.cfg.Listeners.OnVersion != nil {
		p.cfg.Listeners.OnVersion(p, remoteVerMsg)
	}
	return nil
}
func (p *Peer) writeMessage(msg wire.Message, encoding wire.MessageEncoding) error {
	if atomic.LoadInt32(&p.disconnect) != 0 {
		return nil
	}
	fmt.Printf("PEER:Message : %s(Send)\n", msg.Command())
	return wire.MessageWriteEncoding(p.conn, msg, p.protocolVersion, encoding)
}
func (p *Peer) readMessage(encoding wire.MessageEncoding) (wire.Message, []byte, error) {
	msg, buf, err := wire.MessageReadEncoding(p.conn, p.protocolVersion, encoding)
	if err == nil {
		fmt.Printf("PEER:Message : %s(Receivce)\n", msg.Command())
	}
	return msg, buf, err
}
func (p *Peer) negotiateInboundProtocol() error {
	log.Printf("PEER:TODO:negotiateInboundProtocol")
	return nil
}
func (p *Peer) negotiateOutboundProtocol() error {
	if err := p.writeLocalVersionMsg(); err != nil {
		return err
	}

	return p.readRemoteVersionMsg()
}

func (p *Peer) maybeAddDeadline(pendingResponses map[string]time.Time, msgCmd string) {

	deadline := time.Now().Add(stallResponseTimeout)
	switch msgCmd {
	case wire.CmdVersion:
		pendingResponses[wire.CmdVerAck] = deadline
	}
}
func (p *Peer) confirmReadError(err error) bool {
	if atomic.LoadInt32(&p.disconnect) != 0 {
		return false
	}

	// No logging or reject message when the remote peer has been disconnected.
	if err == io.EOF {
		return false
	}
	if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
		return false
	}

	return true
}
func (p *Peer) PushRejectMsg(command string, code wire.RejectCode, reason string, hash *utils.Hash, wait bool) {
	if p.versionKnown && p.protocolVersion < wire.RejectVersion {
		return
	}

	msg := wire.NewMsgReject(command, code, reason)
	if command == wire.CmdTx || command == wire.CmdBlock {
		if hash == nil {
			log.Printf("Sending a reject message for command type %v which should have specified a hash "+
				"but does not", command)
			hash = &utils.Hash{}
		}
		msg.Hash = *hash
	}

	// Send the message without waiting if the caller has not requested it.
	if !wait {
		p.QueueMessage(msg, nil)
		return
	}
	// Send the message and block until it has been sent before returning.
	doneChan := make(chan struct{}, 1)
	p.QueueMessage(msg, doneChan)
	<-doneChan
}
func (p *Peer) PushGetHeadersMsg(locator blockchain.BlockLocator, stopHash *utils.Hash) error {

	msg := wire.NewMsgGetHeaders()
	msg.HashStop = *stopHash
	for _, hash := range locator {
		err := msg.AddBlockLocatorHash(hash)
		if err != nil {
			return err
		}
	}
	p.QueueMessage(msg, nil)
	return nil
}
func (p *Peer) handlePingMsg(msg *wire.MsgPing) {
	// Only reply with pong if the message is from a new enough client.
	if p.protocolVersion > wire.BIP0031Version {
		// Include nonce from ping so pong can be identified.
		p.QueueMessage(wire.NewMsgPong(msg.Nonce), nil)
	}
}
func (p *Peer) handlePongMsg(msg *wire.MsgPong) {
}

func (p *Peer) QueueMessage(msg wire.Message, doneChan chan<- struct{}) {
	// Avoid risk of deadlock if goroutine already exited.  The goroutine
	// we will be sending to hangs around until it knows for a fact that
	// it is marked as disconnected and *then* it drains the channels.

	if !p.Connected() {
		log.Println(" Avoid risk of deadlock")
		if doneChan != nil {
			go func() {
				doneChan <- struct{}{}
			}()
		}
		return
	}

	p.outputQueue <- outMsg{msg: msg, doneChan: doneChan}
}
func (p *Peer) pingHandler() {
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

out:
	for {
		select {
		case <-pingTicker.C:
			nonce, err := wire.RandomUint64()
			if err != nil {
				log.Printf("Not sending ping to %s: %v", p, err)
				continue
			}
			p.QueueMessage(wire.NewMsgPing(nonce), nil)

		case <-p.quit:
			break out
		}
	}
	log.Println("PEER:*******************PingHandler OUT")
}
func (p *Peer) stallHandler() {

	var handlerActive bool
	var handlersStartTime time.Time
	var deadlineOffset time.Duration
	var ioStopped bool
	pendingResponses := make(map[string]time.Time)
	stallTicker := time.NewTicker(stallTickInterval)
	defer stallTicker.Stop()

out:
	for {
		select {
		case msg := <-p.stallControl:
			switch msg.command {
			case sccSendMessage:
				// Add a deadline for the expected response message if needed.
				p.maybeAddDeadline(pendingResponses, msg.message.Command())

			case sccReceiveMessage:
				switch msgCmd := msg.message.Command(); msgCmd {
				default:
					delete(pendingResponses, msgCmd)
				}

			case sccHandlerStart:

				if handlerActive {
					continue
				}

				handlerActive = true
				handlersStartTime = time.Now()

			case sccHandlerDone:

				if !handlerActive {
					continue
				}

				// Extend active deadlines by the time it took to execute the callback.
				duration := time.Since(handlersStartTime)
				deadlineOffset += duration
				handlerActive = false

			default:
				log.Printf("Unsupported message command %v", msg.command)
			}

		case <-stallTicker.C:
			// Calculate the offset to apply to the deadline based
			// on how long the handlers have taken to execute since
			// the last tick.
			now := time.Now()
			offset := deadlineOffset
			if handlerActive {
				offset += now.Sub(handlersStartTime)
			}

			// Disconnect the peer if any of the pending responses don't arrive by their adjusted deadline.
			for command, deadline := range pendingResponses {
				if now.Before(deadline.Add(offset)) {
					continue
				}

				log.Printf("Peer %s appears to be stalled or misbehaving, %s timeout -- disconnecting",
					p, command)
				p.Disconnect()
				break
			}
			// Reset the deadline offset for the next tick.
			deadlineOffset = 0

		case <-p.inQuit:
			if ioStopped {
				break out
			}
			ioStopped = true

		case <-p.outQuit:
			if ioStopped {
				break out
			}
			ioStopped = true
		}
	}

	// Drain any wait channels before going away so there is nothing left waiting on this goroutine.
cleanup:
	for {
		select {
		case <-p.stallControl:
		default:
			break cleanup
		}
	}
	log.Println("PEER:******************StallHandler OUT")
}
func (p *Peer) queueHandler() {
	pendingMsgs := list.New()

	// We keep the waiting flag so that we know if we have a message queued
	// to the outHandler or not.  We could use the presence of a head of
	// the list for this but then we have rather racy concerns about whether
	// it has gotten it at cleanup time - and thus who sends on the
	// message's done channel.  To avoid such confusion we keep a different
	// flag and pendingMsgs only contains messages that we have not yet
	// passed to outHandler.
	waiting := false

	// To avoid duplication below.
	queuePacket := func(msg outMsg, list *list.List, waiting bool) bool {
		if !waiting {
			p.sendQueue <- msg
		} else {
			list.PushBack(msg)
		}
		// we are always waiting now.
		return true
	}
out:
	for {
		select {
		case msg := <-p.outputQueue:
			waiting = queuePacket(msg, pendingMsgs, waiting)

			// This channel is notified when a message has been sent across the network socket.
		case <-p.sendDoneQueue:
			//Do nothing
			next := pendingMsgs.Front()
			if next == nil {
				waiting = false
				continue
			}
			// Notify the outHandler about the next item to asynchronously send.
			val := pendingMsgs.Remove(next)
			p.sendQueue <- val.(outMsg)
		case <-p.quit:
			break out
		}
	}

	// Drain any wait channels before we go away so we don't leave something waiting for us.
	for e := pendingMsgs.Front(); e != nil; e = pendingMsgs.Front() {
		val := pendingMsgs.Remove(e)
		msg := val.(outMsg)
		if msg.doneChan != nil {
			msg.doneChan <- struct{}{}
		}
	}
cleanup:
	for {
		select {
		//clean outputQueue
		case msg := <-p.outputQueue:
			if msg.doneChan != nil {
				msg.doneChan <- struct{}{}
			}

			// sendDoneQueue is buffered so doesn't need draining.
		default:
			break cleanup
		}
	}
	close(p.queueQuit)
	log.Println("PEER:******************QueueHandler OUT")
}
func (p *Peer) outHandler() {
out:
	for {
		select {
		case msg := <-p.sendQueue:
			p.stallControl <- stallControlMsg{sccSendMessage, msg.msg}
			if err := p.writeMessage(msg.msg, msg.encoding); err != nil {
				p.Disconnect()
				log.Printf("Failed to send message to %s: %v", p, err)
				if msg.doneChan != nil {
					msg.doneChan <- struct{}{}
				}
				continue
			}

			if msg.doneChan != nil {
				msg.doneChan <- struct{}{}
			}
			p.sendDoneQueue <- struct{}{}
		case <-p.quit:
			break out

		}
	}
	<-p.queueQuit

	// Drain any wait channels before we go away so we don't leave something waiting for us. We have waited on queueQuit and
	// thus we can be sure that we will not miss anything sent on sendQueue.
cleanup:
	for {
		select {
		case msg := <-p.sendQueue:
			if msg.doneChan != nil {
				msg.doneChan <- struct{}{}
			}
		default:
			break cleanup
		}
	}
	close(p.outQuit)
	log.Println("PEER:********************OutHandler OUT")
}
func (p *Peer) inHandler() {
	idleTimer := time.AfterFunc(idleTimeout, func() {
		fmt.Printf("Peer %s no answer for Timeout %s: ", p, idleTimeout)
		p.Disconnect()
	})
out:
	for atomic.LoadInt32(&p.disconnect) == 0 {
		rmsg, buf, err := p.readMessage(p.wireEncoding)
		idleTimer.Stop()
		if err != nil {
			// Only log the error and send reject message if the local peer is not forcibly disconnecting and
			// the remote peer has not disconnected.
			if p.confirmReadError(err) {
				errMsg := fmt.Sprintf("PEERIN:Can't read message from %s: %v", p, err)
				if err != io.ErrUnexpectedEOF {
					log.Printf(errMsg)
				}
				// NOTE: Ideally this would include the command in the header if
				// at least that much of the message was valid, but that is not
				// currently exposed by wire, so just used malformed for the command.

				//p.PushRejectMsg("malformed", wire.RejectMalformed, errMsg, nil, true)
			}
			break out
		}
		p.stallControl <- stallControlMsg{sccReceiveMessage, rmsg}

		// Handle each supported message type.
		p.stallControl <- stallControlMsg{sccHandlerStart, rmsg}

		switch msg := rmsg.(type) {
		case *wire.MsgVersion:
			p.PushRejectMsg(msg.Command(), wire.RejectDuplicate, "duplicat message version", nil, true)
		case *wire.MsgGetHeaders:
			if p.cfg.Listeners.OnGetHeaders != nil {
				p.cfg.Listeners.OnGetHeaders(p, msg)
			}
		case *wire.MsgHeaders:
			if p.cfg.Listeners.OnHeaders != nil {
				p.cfg.Listeners.OnHeaders(p, msg)
			}
		case *wire.MsgBlock:
			if p.cfg.Listeners.OnBlock != nil {
				p.cfg.Listeners.OnBlock(p, msg, buf)
			}
		case *wire.MsgVerAck:
		case *wire.MsgPing:
			p.handlePingMsg(msg)
		case *wire.MsgPong:
			p.handlePongMsg(msg)
		case *wire.MsgReject:
		case *wire.MsgSendHeaders:
		case *wire.MsgAlert:
		case *wire.MsgAddr:
		case *wire.MsgInv:
			for i, inv := range msg.InvList {
				fmt.Printf("MsgInv%d Type is %d", i, inv.Type)
			}
			fmt.Println()

		default:
			log.Printf("Received unhandled message of type %v from %v", rmsg.Command(), p)
		}
		p.stallControl <- stallControlMsg{sccHandlerDone, rmsg}
		idleTimer.Reset(idleTimeout)
	}

	// Ensure the idle timer is stopped to avoid leaking the resource.
	idleTimer.Stop()
	p.Disconnect()
	close(p.inQuit)
	log.Println("PEER:*********************InHandler OUT")
}

func (p *Peer) start() error {
	log.Printf("PEER:Starting peer %s", p)

	negotiateErr := make(chan error)
	go func() {
		if p.inbound {
			negotiateErr <- p.negotiateInboundProtocol()
		} else {
			negotiateErr <- p.negotiateOutboundProtocol()
		}
	}()

	// Negotiate the protocol within the specified negotiateTimeout.
	select {
	case err := <-negotiateErr:
		if err != nil {
			return err
		}
	case <-time.After(negotiateTimeout):
		return errors.New("protocol negotiation timeout")
	}
	log.Printf("PEER:Connected to %s", p)

	// The protocol has been negotiated successfully so start processing input
	// and output messages.
	go p.stallHandler()
	go p.inHandler()
	go p.queueHandler()
	go p.outHandler()
	go p.pingHandler()

	// Send our verack message now that the IO processing machinery has started.
	p.QueueMessage(wire.NewMsgVerAck(), nil)
	return nil
}
func (p *Peer) AssociateConnection(conn net.Conn) {
	// Already connected?
	if !atomic.CompareAndSwapInt32(&p.connected, 0, 1) {
		return
	}
	p.conn = conn
	go func() {
		if err := p.start(); err != nil {
			log.Printf("PEER:Cannot start peer %v: %v", p, err)
			p.Disconnect()
		}
	}()
}
func (p *Peer) Connected() bool {
	return atomic.LoadInt32(&p.connected) != 0 &&
		atomic.LoadInt32(&p.disconnect) == 0
}
func (p *Peer) Disconnect() {
	if atomic.AddInt32(&p.disconnect, 1) != 1 {
		return
	}

	log.Printf("PEER:Disconnecting %s", p)
	if atomic.LoadInt32(&p.connected) != 0 {
		p.conn.Close()
	}
	close(p.quit)
}
func (p *Peer) WaitForDisconnect() {
	<-p.quit
}
func newPeerBase(origCfg *Config, inbound bool) *Peer {
	cfg := *origCfg // Copy to avoid mutating caller.
	if cfg.ProtocolVersion == 0 {
		cfg.ProtocolVersion = wire.ProtocolVersion
	}

	p := Peer{
		inbound:         inbound,
		wireEncoding:    wire.BaseEncoding,
		stallControl:    make(chan stallControlMsg, 1), // nonblocking sync
		outputQueue:     make(chan outMsg, outputBufferSize),
		sendQueue:       make(chan outMsg, 1),   // nonblocking sync
		sendDoneQueue:   make(chan struct{}, 1), // nonblocking sync
		inQuit:          make(chan struct{}),
		queueQuit:       make(chan struct{}),
		outQuit:         make(chan struct{}),
		quit:            make(chan struct{}),
		cfg:             cfg, // Copy so caller can't mutate.
		services:        cfg.Services,
		protocolVersion: cfg.ProtocolVersion,
	}
	return &p
}
func NewOutboundPeer(cfg *Config, addr string) (*Peer, error) {
	p := newPeerBase(cfg, false)
	p.addr = addr

	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return nil, err
	}
	p.na = wire.NewNetAddressIPPort(net.ParseIP(host), uint16(port), cfg.Services)

	return p, nil
}
