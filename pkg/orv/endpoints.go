package orv

import (
	"context"
	"errors"
	"net/http"
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
func (vk *VaultKeeper) buildRoutes() {
	// Handle POST requests on /hello
	huma.Post(vk.endpoint.api, HELLO, vk.handleHello)

	// Handle GET requests on /status (using the more advanced .Register() method)
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   "status",
		Method:        http.MethodGet,
		Path:          STATUS,
		Summary:       "", // TODO
		Tags:          []string{"meta"},
		DefaultStatus: http.StatusOK,
	}, vk.handleStatus)

	// handle POST requests on /join
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   JOIN[1:],
		Method:        http.MethodPost,
		Path:          JOIN,
		Summary:       "", // TODO docuemntation
		DefaultStatus: http.StatusAccepted,
	}, vk.handleJoin)

}

//#region HELLO

// Request for /hello.
// Used by nodes to introduce themselves to the tree.
// Theoretically, this could be a broadcast and the requester could then pick which HELLO response to follow up on
type HelloReq struct {
	Body struct {
		Id uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
	}
}

// Response for /hello
type HelloResp struct {
	// any fields outside of the body are placed in the header
	// we don't really plan to make use of the header
	Body struct {
		Id uint64 `json:"id" required:"true" example:"123" doc:"unique identifier for the VK"`
		//Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Height uint16 `json:"height" required:"true" example:"8" doc:"the height of the node answering the greeting"`
	}
}

// Handle requests against the HELLO endpoint
func (vk *VaultKeeper) handleHello(ctx context.Context, req *HelloReq) (*HelloResp, error) {
	// validate their ID
	if req.Body.Id == 0 {
		return nil, HErrBadID(req.Body.Id, pt_HELLO_ACK)
	}

	// register the id in the HELLO map
	vk.pendingHellos.Store(vk.id, time.Now())

	vk.heightRWMu.RLock()
	resp := &HelloResp{Body: struct {
		Id     uint64 "json:\"id\" required:\"true\" example:\"123\" doc:\"unique identifier for the VK\""
		Height uint16 "json:\"height\" required:\"true\" example:\"8\" doc:\"the height of the node answering the greeting\""
	}{
		Id:     vk.id,
		Height: vk.height,
	}}
	vk.heightRWMu.RUnlock()

	return resp, nil
}

//#endregion HELLO

//#region STATUS

// Request for /status.
// Used by clients and tests to fetch information about the current state of a vk.
type StatusReq struct {
}

// Response for GET /status commands.
// Returns the status of the current node.
// Used query node info for some tests.
type StatusResp struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

// Handle requests against the status endpoint
func (vk *VaultKeeper) handleStatus(ctx context.Context, req *StatusReq) (*StatusResp, error) {
	resp := &StatusResp{}

	resp.Body.Message = "TODO"
	// TODO

	return resp, nil
}

//#endregion STATUS

//#region JOIN

// Request for /join.
// Used by nodes to ask to join the vault after introducing themselves with HELLO.
type JoinReq struct {
	Body struct {
		Id     uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
		Height uint16 `json:"height" required:"true" example:"3" doc:"height of the node attempting to join the vault"`
	}
}

// Response for /join
type JoinAcceptResp struct {
	PktType string `header:"Packet-Type"` // JOIN_ACCEPT
	Body    struct {
		Id uint64 `json:"id" example:"123" doc:"unique identifier for the VK"`
		//Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Height uint16 `json:"height" example:"8" doc:"the height of the node answering the greeting"`
	}
}

// Handle requests against the HELLO endpoint
func (vk *VaultKeeper) handleJoin(ctx context.Context, req *JoinReq) (*JoinAcceptResp, error) {
	// validate parameters
	if req.Body.Id == 0 {
		return nil, HErrBadID(req.Body.Id, pt_JOIN_DENY)
	}
	vk.heightRWMu.RLock()
	defer vk.heightRWMu.RUnlock()
	if req.Body.Height != vk.height-1 {
		return nil, HErrBadHeight(vk.height, req.Body.Height, pt_JOIN_DENY)
	}

	// check the pendingHello table for this id
	if _, ok := vk.pendingHellos.Load(req.Body.Id); !ok {
		return nil, huma.Error400BadRequest("JOIN_DENY", errors.New("must send HELLO first"))
	}

	resp := &JoinAcceptResp{PktType: "JOIN_ACCEPT", Body: struct {
		Id     uint64 "json:\"id\" example:\"123\" doc:\"unique identifier for the VK\""
		Height uint16 "json:\"height\" example:\"8\" doc:\"the height of the node answering the greeting\""
	}{
		vk.id,
		vk.height,
	}}

	return resp, nil
}

//#endregion JOIN
