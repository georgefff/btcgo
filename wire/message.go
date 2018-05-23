package wire

import (
	"io"
	"fmt"
	"bytes"
	"github.com/georgefff/btcgo/blockchain/utils"
	"errors"
)

// MessageHeaderSize is the number of bytes in a bitcoin message header.
// Bitcoin network (magic) 4 bytes + command 12 bytes + payload length 4 bytes +
// checksum 4 bytes.
const MessageHeaderSize = 24

// CommandSize is the fixed size of all commands in the common bitcoin message
// header.  Shorter commands must be zero padded.
const CommandSize = 12

// MaxMessagePayload is the maximum bytes a message can be regardless of other
// individual limits imposed by messages themselves.
const MaxMessagePayload = (1024 * 1024 * 32) // 32MB
const (
	CmdVersion     = "version"
	CmdVerAck      = "verack"
	CmdReject      = "reject"
	CmdBlock       = "block"
	CmdTx          = "tx"
	CmdPing        = "ping"
	CmdPong        = "pong"
	CmdAddr        = "addr"
	CmdGetHeaders  = "getheaders"
	CmdHeaders     = "headers"
	CmdSendHeaders = "sendheaders"
	CmdAlert       = "alert"
	CmdInv         = "inv"
	CmdGetData     = "getdata"
)

type Message interface {
	BtcDecode(io.Reader, uint32, MessageEncoding) error
	BtcEncode(io.Writer, uint32, MessageEncoding) error
	Command() string
}
type messageHeader struct {
	magic    BitcoinNet
	command  string
	length   uint32
	checksum [4]byte
}

// discardInput reads n bytes from reader r in chunks and discards the read bytes.
// This is used to skip payloads when various errors occur and helps
// prevent rogue nodes from causing massive memory allocation through forging header length.
func discardInput(r io.Reader, n uint32) {
	maxSize := uint32(10 * 1024) // 10k at a time
	numReads := n / maxSize
	bytesRemaining := n % maxSize
	if n > 0 {
		buf := make([]byte, maxSize)
		for i := uint32(0); i < numReads; i++ {
			io.ReadFull(r, buf)
		}
	}
	if bytesRemaining > 0 {
		buf := make([]byte, bytesRemaining)
		io.ReadFull(r, buf)
	}
}
func makeEmptyMessage(command string) (Message, error) {
	var msg Message
	switch command {
	case CmdVersion:
		msg = &MsgVersion{}
	case CmdVerAck:
		msg = &MsgVerAck{}
	case CmdReject:
		msg = &MsgReject{}
	case CmdPing:
		msg = &MsgPing{}
	case CmdPong:
		msg = &MsgPong{}
	case CmdAddr:
		msg = &MsgAddr{}
	case CmdSendHeaders:
		msg = &MsgSendHeaders{}

	case CmdHeaders:
		msg = &MsgHeaders{}
	case CmdGetHeaders:
		msg = &MsgGetHeaders{}
	case CmdBlock:
		msg = &MsgBlock{}
	case CmdAlert:
		msg = &MsgAlert{}
	case CmdInv:
		msg = &MsgInv{}
	default:
		return nil, messageError("makeEmptyMessage", fmt.Sprintf("invaild command: %s", command))
	}
	return msg, nil
}
func MessageWriteEncoding(w io.Writer, msg Message, pver uint32, encoding MessageEncoding) (error) {

	//Encode the message payload
	var pBuffer bytes.Buffer
	if err := msg.BtcEncode(&pBuffer, pver, encoding); err != nil {
		return err
	}
	payload := pBuffer.Bytes()
	lenp := len(payload)
	if lenp > MaxMessagePayload {
		return errors.New("message is to large")
	}

	// Enforce max command size.
	var command [CommandSize]byte
	cmd := msg.Command()
	copy(command[:], []byte(cmd))
	msgHdr := messageHeader{}
	msgHdr.magic = MainNet
	msgHdr.command = cmd
	msgHdr.length = uint32(lenp)
	copy(msgHdr.checksum[:], utils.DoubleHashB(payload)[0:4])

	// Encode the header for the message.
	hBuffer := bytes.NewBuffer(make([]byte, 0, MessageHeaderSize))
	writeElements(hBuffer, msgHdr.magic, command, msgHdr.length, msgHdr.checksum)

	// Write header.
	if _, err := w.Write(hBuffer.Bytes()); err != nil {
		return err
	}
	// Write payload
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}
func readMessageHeader(r io.Reader) (*messageHeader, error) {
	var headerBytes [MessageHeaderSize]byte
	if _, err := io.ReadFull(r, headerBytes[:]); err != nil {
		return nil, err
	}
	hBuffer := bytes.NewReader(headerBytes[:])
	msgHdr := &messageHeader{}
	var command [CommandSize]byte

	readElements(hBuffer, &msgHdr.magic, &command, &msgHdr.length, &msgHdr.checksum)
	msgHdr.command = string(bytes.TrimRight(command[:], string(0)))

	return msgHdr, nil
}
func MessageReadEncoding(r io.Reader, pver uint32, encoding MessageEncoding) (Message, []byte, error) {

	msgHdr, err := readMessageHeader(r)
	if err != nil {
		return nil, nil, err
	}
	if msgHdr.length > MaxMessagePayload {
		str := fmt.Sprintf("message payload is to large - header indicates %d bytes", msgHdr.length)
		return nil, nil, messageError("ReadMessage", str)
	}
	if msgHdr.magic != MainNet {
		discardInput(r, msgHdr.length)
		str := fmt.Sprintf("message from other network [%v]", msgHdr.magic)
		return nil, nil, messageError("ReadMessage", str)
	}

	payload := make([]byte, msgHdr.length)
	_, err = io.ReadFull(r, payload)
	if err != nil {
		return nil, nil, err
	}
	checksum := utils.DoubleHashB(payload)[0:4]
	if !bytes.Equal(checksum[:], msgHdr.checksum[:]) {
		str := fmt.Sprintf("payload checksum err - header,msgHdr.checksum[]: %v", msgHdr.checksum)
		return nil, nil, messageError("ReadMessage", str)
	}

	command := msgHdr.command
	msg, err := makeEmptyMessage(command)
	if err != nil {
		discardInput(r, msgHdr.length)
		return nil, nil, err
	}
	pBuffer := bytes.NewBuffer(payload)
	err = msg.BtcDecode(pBuffer, pver, encoding)
	if err != nil {
		return nil, nil, err
	}

	return msg, payload, nil
}
