// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"
)

// MaxAddrPerMsg is the maximum number of addresses that can be in a single
// bitcoin addr message (MsgAddr).
const MaxAddrPerMsg = 1000

type MsgAddr struct {
	AddrList []*NetAddress
}

func (msg *MsgAddr) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	return nil
}

func (msg *MsgAddr) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	return nil
}

func (msg *MsgAddr) Command() string {
	return CmdAddr
}

func NewMsgAddr() *MsgAddr {
	return &MsgAddr{
		AddrList: make([]*NetAddress, 0, MaxAddrPerMsg),
	}
}
