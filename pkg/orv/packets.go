package orv

type PacketType = string

const (
	pt_HELLO_ACK   PacketType = "HELLO_ACK"
	pt_JOIN_ACCEPT PacketType = "JOIN_ACCEPT"
	pt_JOIN_DENY   PacketType = "JOIN_DENY"
)
