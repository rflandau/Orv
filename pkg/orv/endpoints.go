package orv

import "context"

type Endpoint = string

const (
	HELLO Endpoint = "/HELLO"
)

// Request for /hello.
// Used by nodes to introduce themselves to the tree.
// Theoretically, this could be a broadcast and the requester could then pick which HELLO response to follow up on
type HelloReq struct {
	Id uint64 `json:"id" required:"true" example:"718926735" doc:"unique identifier for this specific node"`
}

// Response for /hello
type RespHello struct {
	// any fields outside of the body are expected to be in the header
	// we don't really plan to make use of the header
	Body struct {
		Id uint64 `json:"id" required:"true" example:"123" doc:"unique identifier for the VK"`
		//Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Error  string `json:"error,omitempty" example:"bad identifier (0)" doc:"the hello request was malformed or invalid"`
		Height uint16 `json:"height" required:"true" example:"8" doc:"the height of the node answering the greeting"`
	}
}

// Response for GET /status commands.
// Returns the status of the current node.
// TODO create handleSTATUS as a method on vk to return information about the status of the node
// we can use this endpoint to query node info in our tests.
type RespStatus struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

func handleHELLO(ctx context.Context, req *HelloReq) (*RespHello, error) {
	resp := &RespHello{}

	// validate their ID
	if req.Id == 0 {
		resp.Body.Error = ErrBadID
		return resp, nil
	}

	//resp.Body
	return resp, nil
}
