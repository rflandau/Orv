package orv

/*
This file contains requests that can be called on VKs from any client.
*/

import (
	"strings"

	"resty.dev/v3"
)

// Spawns a new resty client and uses it to make a status request against the target address.
// Serves as an example of how to make client-side calls against a Vault.
//
// addrStr should be of the form "http://<ip>:<port>"
func Status(addrStr string) (*resty.Response, StatusResp, error) {
	cli := resty.New()

	// compose the url
	addrStr = strings.TrimSuffix(addrStr, "/")
	url := addrStr + EP_STATUS

	sr := StatusResp{}

	res, err := cli.R().
		SetExpectResponseContentType(CONTENT_TYPE).
		SetResult(&(sr.Body)).
		Get(url)
	return res, sr, err

}
