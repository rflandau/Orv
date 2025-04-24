package orv

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

type Endpoint = string

const (
	HELLO  Endpoint = "/hello"
	STATUS Endpoint = "/status"
	JOIN   Endpoint = "/join"
)

// Generates endpoint handling on the given api instance.
// Directly alters a shared pointer within the parameter
// (hence no return value and no pointer parameter (yes, I know it is weird. Weird design decision on huma's part)).
func (vk *VaultKeeper) buildRoutes() {
	// Handle GET requests on /hello
	huma.Post(vk.endpoint.api, HELLO, handleHELLO) // TODO switch handleHello to be a method on vk

	// Handle GET requests on /status (using the more advanced .Register() method)
	huma.Register(vk.endpoint.api, huma.Operation{
		OperationID:   "status",
		Method:        http.MethodGet,
		Path:          STATUS,
		Summary:       "", // TODO
		Tags:          []string{"meta"},
		DefaultStatus: http.StatusOK,
	}, vk.handleStatus)
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
func handleHELLO(ctx context.Context, req *HelloReq) (*HelloResp, error) {
	// validate their ID
	if req.Body.Id == 0 {
		return nil, huma.Error400BadRequest(ErrBadID)
	}

	resp := &HelloResp{}

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
// TODO create handleSTATUS as a method on vk to return information about the status of the node
// we can use this endpoint to query node info in our tests.
type StatusResp struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

// Handle requests against the HELLO endpoint
func (vk *VaultKeeper) handleStatus(ctx context.Context, req *StatusReq) (*StatusResp, error) {
	resp := &StatusResp{}

	resp.Body.Message = "TODO"
	// TODO

	return resp, nil
}

//#endregion STATUS
