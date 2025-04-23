package orv

import "context"

type Endpoint = string

const (
	HELLO Endpoint = "/HELLO"
)

// Request for /hello.
// Used by nodes to introduce themselves to the tree.
type HelloReq struct {
	id uint64 `example:"718926735" doc:"unique identifier for this specific node"`
}

// Response for /hello
type RespHello struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"response to a greeting"`
		Error   string `json:"error" example:"bad identifier (0)" doc:"the hello request was malformed or invalid"`
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
	if req.id == 0 {
		resp.Body.Error = ErrBadID
		return resp, nil
	}

	resp.Body.Message = "Hello, Orv!"
	return resp, nil
}
