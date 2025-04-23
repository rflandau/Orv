package orv

import "context"

type Endpoint = string

const (
	HELLO Endpoint = "/HELLO"
)

// Response for /hello
type helloOut struct {
	Body struct {
		Message string `json:"message" example:"Hello, world!" doc:"Greeting message"`
	}
}

func handleHELLO(ctx context.Context, input *struct{}) (*helloOut, error) {
	resp := &helloOut{}
	//resp.Body.Message = "Hello, Orv!"
	return resp, nil
}
