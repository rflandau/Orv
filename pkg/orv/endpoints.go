package orv

import "context"

type Endpoint = string

const (
	HELLO Endpoint = "/HELLO"
)

// Response for /hello
type RespHello struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
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

func handleHELLO(ctx context.Context, input *struct{}) (*RespHello, error) {
	resp := &RespHello{}
	resp.Body.Message = "Hello, Orv!"
	return resp, nil
}
