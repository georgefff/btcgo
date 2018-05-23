// Copyright (c) 2013-2015 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package wire

import (
	"io"
)

// MsgAlert contains a payload and a signature:
//
//        ===============================================
//        |   Field         |   Data Type   |   Size    |
//        ===============================================
//        |   payload       |   []uchar     |   ?       |
//        -----------------------------------------------
//        |   signature     |   []uchar     |   ?       |
//        -----------------------------------------------
//
// Here payload is an Alert serialized into a byte array to ensure that
// versions using incompatible alert formats can still relay
// alerts among one another.
//
// An Alert is the payload deserialized as follows:
//
//        ===============================================
//        |   Field         |   Data Type   |   Size    |
//        ===============================================
//        |   Version       |   int32       |   4       |
//        -----------------------------------------------
//        |   RelayUntil    |   int64       |   8       |
//        -----------------------------------------------
//        |   Expiration    |   int64       |   8       |
//        -----------------------------------------------
//        |   ID            |   int32       |   4       |
//        -----------------------------------------------
//        |   Cancel        |   int32       |   4       |
//        -----------------------------------------------
//        |   SetCancel     |   set<int32>  |   ?       |
//        -----------------------------------------------
//        |   MinVer        |   int32       |   4       |
//        -----------------------------------------------
//        |   MaxVer        |   int32       |   4       |
//        -----------------------------------------------
//        |   SetSubVer     |   set<string> |   ?       |
//        -----------------------------------------------
//        |   Priority      |   int32       |   4       |
//        -----------------------------------------------
//        |   Comment       |   string      |   ?       |
//        -----------------------------------------------
//        |   StatusBar     |   string      |   ?       |
//        -----------------------------------------------
//        |   Reserved      |   string      |   ?       |
//        -----------------------------------------------
//        |   Total  (Fixed)                |   45      |
//        -----------------------------------------------
//
// NOTE:
//      * string is a VarString i.e VarInt length followed by the string itself
//      * set<string> is a VarInt followed by as many number of strings
//      * set<int32> is a VarInt followed by as many number of ints
//      * fixedAlertSize = 40 + 5*min(VarInt)  = 40 + 5*1 = 45
//
// Now we can define bounds on Alert size, SetCancel and SetSubVer

// maxSignatureSize is the max size of an ECDSA signature.
// NOTE: Since this size is fixed and < 255, the size of VarInt required = 1.
const maxSignatureSize = 72

// maxAlertSize is the maximum size an alert.
//
// MessagePayload = VarInt(Alert) + Alert + VarInt(Signature) + Signature
// MaxMessagePayload = maxAlertSize + max(VarInt) + maxSignatureSize + 1
const maxAlertSize = MaxMessagePayload - maxSignatureSize - MaxVarIntPayload - 1

type Alert struct {
	// Alert format version
	Version int32

	// Timestamp beyond which nodes should stop relaying this alert
	RelayUntil int64

	// Timestamp beyond which this alert is no longer in effect and
	// should be ignored
	Expiration int64

	// A unique ID number for this alert
	ID int32

	// All alerts with an ID less than or equal to this number should
	// cancelled, deleted and not accepted in the future
	Cancel int32

	// All alert IDs contained in this set should be cancelled as above
	SetCancel []int32

	// This alert only applies to versions greater than or equal to this
	// version. Other versions should still relay it.
	MinVer int32

	// This alert only applies to versions less than or equal to this version.
	// Other versions should still relay it.
	MaxVer int32

	// If this set contains any elements, then only nodes that have their
	// subVer contained in this set are affected by the alert. Other versions
	// should still relay it.
	SetSubVer []string

	// Relative priority compared to other alerts
	Priority int32

	// A comment on the alert that is not displayed
	Comment string

	// The alert message that is displayed to the user
	StatusBar string

	// Reserved
	Reserved string
}

// MsgAlert  implements the Message interface and defines a bitcoin alert
// message.
//
// This is a signed message that provides notifications that the client should
// display if the signature matches the key.  bitcoind/bitcoin-qt only checks
// against a signature from the core developers.
type MsgAlert struct {
	// SerializedPayload is the alert payload serialized as a string so that the
	// version can change but the Alert can still be passed on by older
	// clients.
	SerializedPayload []byte

	// Signature is the ECDSA signature of the message.
	Signature []byte

	// Deserialized Payload
	Payload *Alert
}

// BtcDecode decodes r using the bitcoin protocol encoding into the receiver.
// This is part of the Message interface implementation.
func (msg *MsgAlert) BtcDecode(r io.Reader, pver uint32, enc MessageEncoding) error {
	return nil
}

func (msg *MsgAlert) BtcEncode(w io.Writer, pver uint32, enc MessageEncoding) error {
	return nil
}

// Command returns the protocol command string for the message.  This is part
// of the Message interface implementation.
func (msg *MsgAlert) Command() string {
	return CmdAlert
}

// NewMsgAlert returns a new bitcoin alert message that conforms to the Message
// interface.  See MsgAlert for details.
func NewMsgAlert(serializedPayload []byte, signature []byte) *MsgAlert {
	return &MsgAlert{
		SerializedPayload: serializedPayload,
		Signature:         signature,
		Payload:           nil,
	}
}
