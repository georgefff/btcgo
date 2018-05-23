package wire

const (
	// ProtocolVersion is the latest protocol version this package supports.
	ProtocolVersion uint32 = 70012

	// MultipleAddressVersion is the protocol version which added multiple
	// addresses per message (pver >= MultipleAddressVersion).
	MultipleAddressVersion uint32 = 209

	// NetAddressTimeVersion is the protocol version which added the
	// timestamp field (pver >= NetAddressTimeVersion).
	NetAddressTimeVersion uint32 = 31402

	// BIP0031Version is the protocol version AFTER which a pong message
	// and nonce field in ping were added (pver > BIP0031Version).
	BIP0031Version uint32 = 60000

	// BIP0035Version is the protocol version which added the mempool
	// message (pver >= BIP0035Version).
	BIP0035Version uint32 = 60002

	// BIP0037Version is the protocol version which added new connection
	// bloom filtering related messages and extended the version message
	// with a relay flag (pver >= BIP0037Version).
	BIP0037Version uint32 = 70001

	// RejectVersion is the protocol version which added a new reject
	// message.
	RejectVersion uint32 = 70002

	// BIP0111Version is the protocol version which added the SFNodeBloom
	// service flag.
	BIP0111Version uint32 = 70011

	// SendHeadersVersion is the protocol version which added a new
	// sendheaders message.
	SendHeadersVersion uint32 = 70012

	// FeeFilterVersion is the protocol version which added a new
	// feefilter message.
	FeeFilterVersion uint32 = 70013
)

// ServiceFlag identifies services supported by a bitcoin peer.
type ServiceFlag uint64

const (
	// SFNodeNetwork is a flag used to indicate a peer is a full node.
	SFNodeNetwork ServiceFlag = 1 << iota

	// SFNodeGetUTXO is a flag used to indicate a peer supports the getutxos and utxos commands (BIP0064).
	SFNodeGetUTXO

	// SFNodeBloom is a flag used to indicate a peer supports bloom filtering.
	SFNodeBloom

	// SFNodeWitness is a flag used to indicate a peer supports blocks
	// and transactions including witness data (BIP0144).
	SFNodeWitness
)

// MessageEncoding represents the wire message encoding format to be used.
type MessageEncoding uint32

const (
	// BaseEncoding encodes all messages in the default format specified for the Bitcoin wire protocol.
	BaseEncoding MessageEncoding = 1 << iota

	// For transaction messages, the new encoding format detailed in BIP0144 will be used.
	WitnessEncoding
)

const (
	DefaultPortString = "8333"
	DefaultPortInt    = 8333
)

// BitcoinNet represents which bitcoin network a message belongs to.
type BitcoinNet uint32

const (
	MainNet  BitcoinNet = 0xd9b4bef9
	TestNet  BitcoinNet = 0xdab5bffa
	TestNet3 BitcoinNet = 0x0709110b
	SimNet   BitcoinNet = 0x12141c16
)
