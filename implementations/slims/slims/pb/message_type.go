package pb

// String returns the string representation of the given MessageType.
// It is just a big switch statement.
func (mt MessageType) String() string {
	switch mt {
	case MessageType_mt_FAULT:
		return "FAULT"
	case MessageType_mt_HELLO:
		return "HELLO"
	case HelloAck:
		return "HELLO_ACK"
	case Join:
		return "JOIN"
	case JoinAccept:
		return "JOIN_ACCEPT"
	case Register:
		return "REGISTER"
	case RegisterAccept:
		return "REGISTER_ACCEPT"
	case Merge:
		return "MERGE"
	case MergeAccept:
		return "MERGE_ACCEPT"
	case Increment:
		return "INCREMENT"
	case IncrementAck:
		return "INCREMENT_ACK"
	case ServiceHeartbeat:
		return "SERVICE_HEARTBEAT"
	case ServiceHeartbeatAck:
		return "SERVICE_HEARTBEAT_ACK"
	case VKHeartbeat:
		return "VK_HEARTBEAT"
	case VKHeartbeatAck:
		return "VK_HEARTBEAT_ACK"
	case Status:
		return "STATUS"
	case StatusResp:
		return "STATUS_RESP"
	case List:
		return "LIST"
	case ListResp:
		return "LIST_RESP"
	case Get:
		return "GET"
	case GetResp:
		return "GET_RESP"
	default:
		return "UNKNOWN"
	}
}
