package orv

import (
	"context"
	"net/http"
	"net/netip"
	"time"

	"github.com/danielgtaylor/huma/v2"
)

type Endpoint = string

const (
	HELLO    Endpoint = "/hello"
	STATUS   Endpoint = "/status"
	JOIN     Endpoint = "/join"
	REGISTER Endpoint = "/register"
)

// Generates endpoint handling on the given api instance.
// Directly alters a shared pointer within the parameter
// (hence no return value and no pointer parameter (yes, I know it is weird. Weird design decision on huma's part)).
func (vk *VaultKeeper) buildEndpoints() {
	// Handle POST requests on /hello
	huma.Post(vk.endpoint.api, HELLO, vk.handleHello)

	// Handle GET requests on /status (using the more advanced .Register() method)
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   STATUS[1:],
		Method:        http.MethodGet,
		Path:          STATUS,
		Summary:       STATUS[1:],
		Tags:          []string{"meta"},
		DefaultStatus: http.StatusOK,
	}, vk.handleStatus)

	// handle POST requests on /join
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   JOIN[1:],
		Method:        http.MethodPost,
		Path:          JOIN,
		Summary:       JOIN[1:],
		DefaultStatus: http.StatusAccepted,
	}, vk.handleJoin)

	// handle POST requests on /register
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   REGISTER[1:],
		Method:        http.MethodPost,
		Path:          REGISTER,
		Summary:       REGISTER[1:],
		DefaultStatus: http.StatusAccepted,
	}, vk.handleRegister)
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

	// register the id in the HELLO map
	vk.pendingHellos.Store(vk.id, time.Now())

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
// Used query node info for some tests.
type StatusResp struct {
	PktType PacketType `header:"Packet-Type"` // STATUS_RESPONSE
	Body    struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

// Handle requests against the status endpoint
func (vk *VaultKeeper) handleStatus(ctx context.Context, req *StatusReq) (*StatusResp, error) {
	resp := &StatusResp{PktType: PT_STATUS_RESPONSE}

	resp.Body.Message = "TODO"
	// TODO

	return resp, nil
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

	vk.log.Debug().Uint64("child id", cid).Bool("VK?", req.Body.IsVK).Msg("accepted joined")

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
		Address string `json:"address" example:"172.1.1.54:22" doc:"the address the service is bound to. Only populated from leaf to parent."`
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

// Handle requests against the JOIN endpoint
func (vk *VaultKeeper) handleRegister(_ context.Context, req *RegisterReq) (*RegisterAcceptResp, error) {
	// validate parameters
	var cid uint64 = req.Body.Id
	if cid == 0 {
		return nil, HErrBadID(req.Body.Id, PT_REGISTER_DENY)
	}
	resp := &RegisterAcceptResp{}
	/*
		// do we know this child?
		vk.childrenMu.Lock()
		defer vk.childrenMu.Unlock()
		if _, exists := vk.leaves[cid]; !exists {
			return nil, HErrMustJoin(PT_REGISTER_DENY)
		}
		// HUMA should reject empty services for us
		// ensure we can parse the address and staleness
		serviceAddr, err := netip.ParseAddrPort(req.Body.Address)
		if err != nil {
			return nil, HErrBadAddr(req.Body.Address, PT_REGISTER_DENY)
		}
		staleness, err := time.ParseDuration(req.Body.Stale)
		if err != nil {
			return nil, HErrBadStaleness(req.Body.Stale, PT_REGISTER_DENY)
		}

		// now that we have validated the registration, add this service to the child
		vk.leaves[cid][req.Body.Service] = srv{serviceAddr, staleness}

		resp := &RegisterAcceptResp{
			PktType: PT_REGISTER_ACCEPT,
			Body: struct {
				Id      uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
				Service string "json:\"service\" required:\"true\" example:\"SSH\" doc:\"the name of the service to be registered\""
			}{
				Id:      vk.id,
				Service: req.Body.Service,
			},
		}
	*/

	if !vk.isRoot() {
		// TODO propagate the request up the tree
	}

	return resp, nil
}

//#endregion JOIN
