package orv

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type Endpoint = string

const (
	EP_HELLO             Endpoint = "/hello"
	EP_STATUS            Endpoint = "/status"
	EP_JOIN              Endpoint = "/join"
	EP_REGISTER          Endpoint = "/register"
	EP_VK_HEARTBEAT      Endpoint = "/vk-heartbeat"
	EP_SERVICE_HEARTBEAT Endpoint = "/service-heartbeat"
)

// Generates endpoint handling on the given api instance.
// Directly alters a shared pointer within the parameter
// (hence no return value and no pointer parameter (yes, I know it is weird. Weird design decision on huma's part)).
func (vk *VaultKeeper) buildEndpoints() {
	// Handle POST requests on /hello
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_HELLO[1:],
		Method:        http.MethodPost,
		Path:          EP_HELLO,
		Summary:       EP_HELLO[1:],
		DefaultStatus: http.StatusOK,
	}, vk.handleHello)

	// Handle GET requests on /status
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_STATUS[1:],
		Method:        http.MethodGet,
		Path:          EP_STATUS,
		Summary:       EP_STATUS[1:],
		Tags:          []string{"meta"},
		DefaultStatus: http.StatusOK,
	}, vk.handleStatus)

	// handle POST requests on /join
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_JOIN[1:],
		Method:        http.MethodPost,
		Path:          EP_JOIN,
		Summary:       EP_JOIN[1:],
		DefaultStatus: http.StatusAccepted,
	}, vk.handleJoin)

	// handle POST requests on /register
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_REGISTER[1:],
		Method:        http.MethodPost,
		Path:          EP_REGISTER,
		Summary:       EP_REGISTER[1:],
		DefaultStatus: http.StatusAccepted,
	}, vk.handleRegister)

	// handle heartbeats for child VKs
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_VK_HEARTBEAT[1:],
		Method:        http.MethodPost,
		Path:          EP_VK_HEARTBEAT,
		Summary:       EP_VK_HEARTBEAT[1:],
		DefaultStatus: http.StatusOK,
	}, vk.handleVKHeartbeat)

	// handle heartbeats for leaf services
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   EP_SERVICE_HEARTBEAT[1:],
		Method:        http.MethodPost,
		Path:          EP_SERVICE_HEARTBEAT,
		Summary:       EP_SERVICE_HEARTBEAT[1:],
		DefaultStatus: http.StatusOK,
	}, vk.handleServiceHeartbeat)
}

//#region HELLO

// Request for /hello.
// Used by nodes to introduce themselves to the tree.
// Theoretically, this could be a broadcast and the requester could then pick which HELLO response to follow up on
type HelloReq struct {
	PktType PacketType `header:"Packet-Type"` // HELLO
	Body    struct {
		Id uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
	}
}

// Response for /hello
type HelloResp struct {
	PktType PacketType `header:"Packet-Type"` // HELLO_ACK
	Body    struct {
		Id uint64 `json:"id" required:"true" example:"123" doc:"unique identifier for the VK"`
		//Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Height uint16 `json:"height" required:"true" example:"8" doc:"the height of the node answering the greeting"`
	}
}

// Handle requests against the HELLO endpoint
func (vk *VaultKeeper) handleHello(ctx context.Context, req *HelloReq) (*HelloResp, error) {
	// validate their ID
	if req.Body.Id == 0 {
		return nil, HErrBadID(req.Body.Id, PT_HELLO_ACK)
	}

	vk.log.Debug().Uint64("node id", req.Body.Id).Msg("greeted by node")

	// register the id in the HELLO map
	// each HELLO refreshes the timestamp the pruner uses
	vk.pendingHellos.Store(req.Body.Id, time.Now())

	vk.structureRWMu.RLock()
	resp := &HelloResp{PktType: PT_HELLO_ACK,
		Body: struct {
			Id     uint64 "json:\"id\" required:\"true\" example:\"123\" doc:\"unique identifier for the VK\""
			Height uint16 "json:\"height\" required:\"true\" example:\"8\" doc:\"the height of the node answering the greeting\""
		}{
			Id:     vk.id,
			Height: vk.height,
		}}
	vk.structureRWMu.RUnlock()

	return resp, nil
}

//#endregion HELLO

//#region STATUS

// Request for /status.
// Used by clients and tests to fetch information about the current state of a vk.
type StatusReq struct {
	PktType PacketType `header:"Packet-Type"` // STATUS
}

// Response for GET /status commands.
// Returns the status of the current node.
// All fields (other than Id) are optional and may be omitted at the VK's discretion.
type StatusResp struct {
	PktType PacketType `header:"Packet-Type"` // STATUS_RESPONSE
	Body    struct {
		Id            childID          `json:"id" required:"true" example:"123" doc:"unique identifier for the VK"`
		Height        uint16           `json:"height" example:"8" doc:"the height of the queried VK"`
		Children      ChildrenSnapshot `json:"children" example:"" doc:"the children of this VK and their services. Represents a point-in-time snapshot. No representations are guaranteed and format is left up to the discretion of the VK implementation"`
		ParentID      uint64           `json:"parent-id" example:"789" doc:"unique identifier for the VK's parent. 0 if VK is root."`
		ParentAddress string           `json:"parent-address" example:"111.111.111.111:8080" doc:"address and port of the VK parent's process"`
		PruneTimes    struct {
			PendingHello     string `json:"pending-hello"`
			ServicelessChild string `json:"serviceless-child"`
			CVK              string `json:"child-vault-keeper"`
		} `json:"prune-times" example:"" doc:"this VK's timings for considering associated data to be stale"`
	}
}

// Handle requests against the status endpoint.
// Returns a bunch of information about the queried VK.
func (vk *VaultKeeper) handleStatus(ctx context.Context, req *StatusReq) (*StatusResp, error) {
	vk.structureRWMu.RLock()
	height := vk.height
	parentID := vk.parent.id
	var parentAddress string
	if vk.parent.addr.IsValid() {
		parentAddress = vk.parent.addr.String()
	}
	vk.structureRWMu.RUnlock()

	return &StatusResp{
		PktType: PT_STATUS_RESPONSE,
		Body: struct {
			Id            childID          "json:\"id\" required:\"true\" example:\"123\" doc:\"unique identifier for the VK\""
			Height        uint16           "json:\"height\" example:\"8\" doc:\"the height of the queried VK\""
			Children      ChildrenSnapshot "json:\"children\" example:\"\" doc:\"the children of this VK and their services. Represents a point-in-time snapshot. No representations are guaranteed and format is left up to the discretion of the VK implementation\""
			ParentID      uint64           "json:\"parent-id\" example:\"789\" doc:\"unique identifier for the VK's parent. 0 if VK is root.\""
			ParentAddress string           "json:\"parent-address\" example:\"111.111.111.111:8080\" doc:\"address and port of the VK parent's process\""
			PruneTimes    struct {
				PendingHello     string `json:"pending-hello"`
				ServicelessChild string `json:"serviceless-child"`
				CVK              string `json:"child-vault-keeper"`
			} `json:"prune-times" example:"" doc:"this VK's timings for considering associated data to be stale"`
		}{
			Id:            vk.id,
			Height:        height,
			Children:      vk.children.Snapshot(),
			ParentID:      parentID,
			ParentAddress: parentAddress,
			PruneTimes: struct {
				PendingHello     string `json:"pending-hello"`
				ServicelessChild string `json:"serviceless-child"`
				CVK              string `json:"child-vault-keeper"`
			}{vk.pt.PendingHello.String(), vk.pt.ServicelessChild.String(), vk.pt.CVK.String()},
		},
	}, nil
}

//#endregion STATUS

//#region JOIN

// Request for /join.
// Used by nodes to ask to join the vault after introducing themselves with HELLO.
type JoinReq struct {
	PktType PacketType `header:"Packet-Type"` // JOIN
	Body    struct {
		Id     uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
		Height uint16 `json:"height,omitempty" dependentRequired:"is-vk" example:"3" doc:"height of the vk attempting to join the vault"`
		VKAddr string `json:"vk-addr,omitempty" dependentRequired:"is-vk" example:"174.1.3.4:8080" doc:"address of the listening VK service that can receive INCRs"`
		IsVK   bool   `json:"is-vk,omitempty" example:"false" doc:"is this node a VaultKeeper or a leaf? If true, height and VKAddr are required"`
	}
}

// Response for /join
type JoinAcceptResp struct {
	PktType PacketType `header:"Packet-Type"` // JOIN_ACCEPT
	Body    struct {
		Id uint64 `json:"id" example:"123" doc:"unique identifier for the VK"`
		//Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Height uint16 `json:"height" example:"8" doc:"the height of the requester's new parent"`
	}
}

// Handle requests against the JOIN endpoint
func (vk *VaultKeeper) handleJoin(ctx context.Context, req *JoinReq) (*JoinAcceptResp, error) {
	// validate parameters
	var cid uint64 = req.Body.Id
	if cid == 0 {
		return nil, HErrBadID(req.Body.Id, PT_JOIN_DENY)
	}

	// check the pendingHello table for this id
	if _, ok := vk.pendingHellos.Load(req.Body.Id); !ok {
		return nil, HErrMustHello(PT_JOIN_DENY)
	}

	vk.structureRWMu.RLock()
	defer vk.structureRWMu.RUnlock()

	// check if the node is attempting to join as a vk or a leaf
	if req.Body.IsVK { // is a VK
		//validate VK-specific parameters
		if req.Body.Height != vk.height-1 {
			return nil, HErrBadHeight(vk.height, req.Body.Height, PT_JOIN_DENY)
		}
		addr, err := netip.ParseAddrPort(req.Body.VKAddr)
		if err != nil {
			return nil, HErrBadAddr(req.Body.VKAddr, PT_JOIN_DENY)
		}

		if wasVK, wasLeaf := vk.children.addVK(cid, addr); wasLeaf {
			// if we already have a leaf with the given ID, return failure
			return nil, HErrIDInUse(cid, PT_JOIN_DENY)
		} else if wasVK {
			vk.log.Debug().Uint64("child id", cid).Msg("duplicate join")
		}
	} else { // is a leaf
		if wasVk, wasLeaf := vk.children.addLeaf(cid); wasLeaf {
			// if it was already a leaf, then throw out the join and act like it work because... well... it did
			vk.log.Debug().Uint64("child id", cid).Msg("duplicate join")
		} else if wasVk {
			// if we already have a vk with the given ID, return failure
			return nil, HErrIDInUse(cid, PT_JOIN_DENY)
		}
	}

	vk.log.Debug().Uint64("child id", cid).Bool("VK?", req.Body.IsVK).Msg("accepted JOIN")

	resp := &JoinAcceptResp{
		PktType: "JOIN_ACCEPT",
		Body: struct {
			Id     uint64 "json:\"id\" example:\"123\" doc:\"unique identifier for the VK\""
			Height uint16 "json:\"height\" example:\"8\" doc:\"the height of the requester's new parent\""
		}{
			vk.id,
			vk.height,
		}}

	return resp, nil
}

//#endregion JOIN

//#region REGISTER

// Request for /register.
// Used by nodes to tell their parent about a new service.
type RegisterReq struct {
	PktType PacketType `header:"Packet-Type"` // REGISTER
	Body    struct {
		Id      uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
		Service string `json:"service" required:"true" example:"SSH" doc:"the name of the service to be registered"`
		Address string `json:"address" required:"true" example:"172.1.1.54:22" doc:"the address the service is bound to. Only populated from leaf to parent."`
		Stale   string `json:"stale" example:"1m5s45ms" doc:"after how much time without a heartbeat is this service eligible for pruning"`
	}
}

// Response for /register.
type RegisterAcceptResp struct {
	PktType PacketType `header:"Packet-Type"` // REGISTER_ACCEPT
	Body    struct {
		Id      uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
		Service string `json:"service" required:"true" example:"SSH" doc:"the name of the service to be registered"`
	}
}

// Handle requests against the REGISTER endpoint
func (vk *VaultKeeper) handleRegister(_ context.Context, req *RegisterReq) (*RegisterAcceptResp, error) {
	var (
		err      error
		cid      uint64 = req.Body.Id
		sn       string = strings.TrimSpace(req.Body.Service)
		addrStr  string = strings.TrimSpace(req.Body.Address)
		addr     netip.AddrPort
		staleStr string = strings.TrimSpace(req.Body.Stale)
	)
	// validate parameters other than Stale
	if cid == 0 {
		return nil, HErrBadID(req.Body.Id, PT_REGISTER_DENY)
	} else if sn == "" {
		return nil, HErrBadServiceName(sn, PT_REGISTER_DENY)
	} else if addr, err = netip.ParseAddrPort(addrStr); err != nil {
		return nil, HErrBadAddr(addrStr, PT_REGISTER_DENY)
	}

	err = vk.children.addService(cid, sn, addr, staleStr)
	if err != nil {
		return nil, huma.ErrorWithHeaders(err, http.Header{
			hdrPkt_t: {PT_REGISTER_DENY},
		})
	}

	resp := &RegisterAcceptResp{
		PktType: PT_REGISTER_ACCEPT,
		Body: struct {
			Id      uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
			Service string "json:\"service\" required:\"true\" example:\"SSH\" doc:\"the name of the service to be registered\""
		}{
			vk.id,
			sn,
		},
	}

	if !vk.isRoot() {
		// TODO asynchronously propagate the request up the tree
	}

	return resp, nil
}

//#endregion

//#region VK_HEARTBEAT

// Request for /register.
// Used by nodes to tell their parent about a new service.
type VKHeartbeatReq struct {
	PktType PacketType `header:"Packet-Type"` // VK_HEARTBEAT
	Body    struct {
		Id uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier of the child VK being refreshed"`
	}
}

// Response for /register.
type VKHeartbeatAck struct {
	PktType PacketType `header:"Packet-Type"` // SERVICE_HEARTBEAT_ACK
	Body    struct {
		Id uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier of the child VK being refreshed"`
	}
}

// Handle requests against the REGISTER endpoint
func (vk *VaultKeeper) handleVKHeartbeat(_ context.Context, req *VKHeartbeatReq) (*VKHeartbeatAck, error) {
	resp := &VKHeartbeatAck{Body: struct {
		Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier of the child VK being refreshed\""
	}{Id: vk.id}}
	if err := vk.children.HeartbeatCVK(req.Body.Id); err != nil {
		return nil, huma.ErrorWithHeaders(err, http.Header{
			hdrPkt_t: {PT_VK_HEARTBEAT_FAULT},
		})
	}
	return resp, nil
}

//#endregion VK_HEARTBEAT

//#region SERVICE_HEARTBEAT

// Request for /register.
// Used by nodes to tell their parent about a new service.
type ServiceHeartbeatReq struct {
	PktType PacketType `header:"Packet-Type"` // VK_HEARTBEAT
	Body    struct {
		Id       uint64   `json:"id" required:"true" example:"718926735" doc:"unique identifier of the child VK being refreshed"`
		Services []string `json:"services" required:"true" example:"[\"serviceA\", \"serviceB\"]" doc:"the name of the services to refresh"`
	}
}

// Response for /register.
type ServiceHeartbeatAck struct {
	PktType PacketType `header:"Packet-Type"` // SERVICE_HEARTBEAT_ACK
	Body    struct {
		Id       uint64   `json:"id" required:"true" example:"718926735" doc:"unique identifier of the child VK being refreshed"`
		Services []string `json:"services" required:"true" example:"[\"serviceA\"]" doc:"the name of the services that were successfully refreshed"`
	}
}

// Handle requests against the REGISTER endpoint
func (vk *VaultKeeper) handleServiceHeartbeat(_ context.Context, req *ServiceHeartbeatReq) (*ServiceHeartbeatAck, error) {
	resp := &ServiceHeartbeatAck{}
	// TODO
	return resp, nil
}

//#endregion SERVICE_HEARTBEAT
