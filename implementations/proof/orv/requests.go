package orv

/*
This file contains static subroutine requests that can be called on VKs from any client.
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

// Spawns a new resty client and uses it to make a List request against the target address.
//
// addrStr should be of the form "http://<ip>:<port>"
func List(addrStr string, hopcount uint16) (*resty.Response, ListResponseResp, error) {
	cli := resty.New()

	// compose the url
	addrStr = strings.TrimSuffix(addrStr, "/")
	url := addrStr + EP_LIST

	lrr := ListResponseResp{}

	body := ListReq{Body: struct {
		HopCount uint16 "json:\"hop-count\" example:\"2\" doc:\"the maximum number of VKs to hop to. A hop count of 0 or 1 means the request will stop at the first VK (the VK who receives the initial request)\""
	}{
		HopCount: hopcount,
	}}.Body

	res, err := cli.R().
		SetExpectResponseContentType(CONTENT_TYPE).
		SetResult(&(lrr.Body)).
		SetBody(body).
		Post(url)
	return res, lrr, err

}

// Spawns a new resty client and uses it to make a GET request against the target address for the named service.
//
// vkAddrStr should be of the form "http://<ip>:<port>"
func Get(vkAddrStr string, hopCount uint16, service string) (*resty.Response, GetResponseResp, error) {
	cli := resty.New()

	// compose the url
	vkAddrStr = strings.TrimSuffix(vkAddrStr, "/")
	url := vkAddrStr + EP_GET

	grr := GetResponseResp{}

	body := GetReq{Body: struct {
		Service  string "json:\"service\" required:\"true\" example:\"ssh\" doc:\"the name of the services to be fetched\""
		HopCount uint16 "json:\"hop-count\" required:\"true\" example:\"2\" doc:\"the maximum number of VKs to hop to. A hop count of 0 or 1 means the request will stop at the first VK (the VK who receives the initial request)\""
	}{service, hopCount}}.Body

	res, err := cli.R().
		SetExpectResponseContentType(CONTENT_TYPE).
		SetResult(&(grr.Body)).
		SetBody(body).
		Post(url)
	return res, grr, err
}
