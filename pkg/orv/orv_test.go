package orv_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
)

//#region request helper functions

// POSTs a HELLO to the endpoint embedded in the huma api.
// Only returns if the given status code was matched; Fatal if not
func makeHelloRequest(t *testing.T, targetAPI humatest.TestAPI, expectedCode int, id uint64) (resp *httptest.ResponseRecorder) {
	resp = targetAPI.Post(orv.EP_HELLO,
		"Packet-Type: "+orv.PT_HELLO,
		orv.HelloReq{
			Body: struct {
				Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
			}{
				Id: id,
			}}.Body)
	if resp.Code != expectedCode {
		t.Fatal("hello request failed: " + ErrBadResponseCode(resp.Code, expectedCode))
	}
	return resp
}

// POSTs a JOIN to the endpoint embedded in the huma api.
// Only returns if the given status code was matched; Fatal if not
func makeJoinRequest(t *testing.T, targetAPI humatest.TestAPI, expectedCode int, id uint64, height uint16, vkaddr string, isvk bool) (resp *httptest.ResponseRecorder) {
	resp = targetAPI.Post(orv.EP_JOIN,
		"Packet-Type: "+orv.PT_JOIN,
		orv.JoinReq{
			Body: struct {
				Id     uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
				Height uint16 "json:\"height,omitempty\" dependentRequired:\"is-vk\" example:\"3\" doc:\"height of the vk attempting to join the vault\""
				VKAddr string "json:\"vk-addr,omitempty\" dependentRequired:\"is-vk\" example:\"174.1.3.4:8080\" doc:\"address of the listening VK service that can receive INCRs\""
				IsVK   bool   "json:\"is-vk,omitempty\" example:\"false\" doc:\"is this node a VaultKeeper or a leaf? If true, height and VKAddr are required\""
			}{
				Id:     id,
				Height: height,
				VKAddr: vkaddr,
				IsVK:   isvk,
			},
		}.Body)
	if resp.Code != expectedCode {
		t.Fatal("join request failed: " + ErrBadResponseCode(resp.Code, expectedCode) + "\n" + resp.Body.String())
	}

	return resp
}

// POSTs a REGISTER to the endpoint embedded in the huma api, registering a new service under the given id.
// Only returns if the given status code was matched; Fatal if not
func makeRegisterRequest(t *testing.T, api humatest.TestAPI, expectedCode int, id uint64, sn string, ap netip.AddrPort, stale time.Duration) (resp *httptest.ResponseRecorder) {
	resp = api.Post(orv.EP_REGISTER,
		"Packet-Type: "+orv.PT_REGISTER,
		orv.RegisterReq{
			Body: struct {
				Id      uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
				Service string "json:\"service\" required:\"true\" example:\"SSH\" doc:\"the name of the service to be registered\""
				Address string "json:\"address\" required:\"true\" example:\"172.1.1.54:22\" doc:\"the address the service is bound to. Only populated from leaf to parent.\""
				Stale   string "json:\"stale\" example:\"1m5s45ms\" doc:\"after how much time without a heartbeat is this service eligible for pruning\""
			}{
				Id:      id,
				Service: sn,
				Address: ap.String(),
				Stale:   stale.String(),
			},
		}.Body)
	if resp.Code != expectedCode {
		s := "valid"
		if expectedCode-400 >= 0 {
			s = "invalid"
		}
		t.Fatal(s, " register request failed: ", ErrBadResponseCode(resp.Code, expectedCode))
	}

	return resp
}

//#endregion

// Humatest is quite verbose by default and has no native way to suppress its logging.
// To keep the tests readable (while still gaining the functionality of humatest), we wrap the test handler and give it no-op logging.
// This way, humatest's native output is suppressed, but we can still utilize all the normal features of the test handler
// (using the unwrapped version).
type SuppressedLogTest struct {
	*testing.T
}

// no-op wrapper
func (tb SuppressedLogTest) Log(args ...any) {
	// no-op
}

// no-op wrapper
func (tb SuppressedLogTest) Logf(format string, args ...any) {
	// no-op
}

//#region testing error messages

func ErrBadResponseCode(got, expected int) string {
	return fmt.Sprintf("incorrect response code (got: %d, expected %d)", got, expected)
}

//#endregion

// Marshalls and POSTs data to the given address.
// Returns the status code, byte string body, and an error (if applicable).
//
// Based on Professor Patrick Tague's helper test code.
func getResponse(ip string, port int, endpoint string, data any) (int, []byte, error) {

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return -1, nil, err
	}

	url := "http://" + ip + ":" + strconv.Itoa(port) + endpoint

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return -1, nil, err
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return -1, nil, err
	}
	resp.Body.Close()

	return resp.StatusCode, body, nil
}

func StartVKListener(t *testing.T, api huma.API) (*orv.VaultKeeper, netip.AddrPort) {
	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}

	// spin up vk
	var vkid uint64 = 1
	vk, err := orv.NewVaultKeeper(vkid, vkAddr, orv.SetHumaAPI(api))
	if err != nil {
		t.Fatal(err)
	}
	if err := vk.Start(); err != nil {
		t.Fatal(err)
	}
	return vk, vkAddr
}

// Simple but important test to guarantee proper acceptance and rejection of message types to each endpoint.
// A la ClientArgs in lab3.
func TestEndpointArgs(t *testing.T) {

	slt := SuppressedLogTest{t}
	// spawn the huma test API
	_, api := humatest.New(slt)

	vk, vkAddr := StartVKListener(t, api)

	time.Sleep(1 * time.Second) // give the VK time to start up

	t.Cleanup(vk.Terminate)

	// submit a valid HELLO
	makeHelloRequest(t, api, 200, 2)

	// submit a valid JOIN
	makeJoinRequest(t, api, 200, 2, 0, "", false)

	// submit a HELLO with an invalid ID
	makeHelloRequest(t, api, 400, 0)

	// submit a JOIN with an invalid ID and height
	makeJoinRequest(t, api, 400, 0, 0, "", false)

	// submit a JOIN with same ID
	makeJoinRequest(t, api, 409, 2, 0, "", false)

	// submit a REGISTER with an invalid ID
	makeRegisterRequest(t, api, 400, 3, "Good advice generator - Just drink Milk instead of Coffee", vkAddr, time.Second)

	// submit a REGISTER for an unjoined ID
	makeHelloRequest(t, api, 200, 3)
	makeRegisterRequest(t, api, 400, 3, "Very good Coffee Maker", vkAddr, time.Second)

	fmt.Println("ok")

	// submit a valid REGISTER for vk
	makeHelloRequest(t, api, 200, 100)
	makeJoinRequest(t, api, 200, 100, 1, "", true)
	makeRegisterRequest(t, api, 200, 100, "Horrible Coffee Maker", vkAddr, time.Second)

	// submit a valid REGISTER for vk and check if multiple services are allowed
	makeRegisterRequest(t, api, 200, 100, "Tea Maker - makes sense why I make horrible Coffee", vkAddr, time.Second)

	// submit a invalid REGISTER for vk
	makeRegisterRequest(t, api, 400, 100, "", vkAddr, time.Second)

	fmt.Println("ok")

}

// Tests that a single VK can support multiple leaves and multiple services on each leaf simultaneously.
// Each leaf will HELLO -> JOIN and then submit multiple REGISTERS. Each service will need to send heartbeats to the VK.
// After a short detail, the test checks if the VK still believe that all services are active.
func TestMultiLeafMultiService(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that we can compose LeafA --> VKA --> VKB <-- LeafB, with all working heartbeats and a bubble-up list request.
func TestHopList(t *testing.T) {
	// spawn the test api
	slt := SuppressedLogTest{t}
	// spawn the huma test API
	_, apiA := humatest.New(slt)
	_, apiB := humatest.New(slt)

	vkAAddr, err := netip.ParseAddrPort("[::1]:8090")
	if err != nil {
		t.Fatal(err)
	}
	vkBAddr, err := netip.ParseAddrPort("[::1]:9000")
	if err != nil {
		t.Fatal(err)
	}

	vkA, err := orv.NewVaultKeeper(1, vkAAddr, orv.SetHumaAPI(apiA))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vkA.Terminate)
	vkB, err := orv.NewVaultKeeper(2, vkBAddr, orv.Height(1), orv.SetHumaAPI(apiB))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vkB.Terminate)
	// Join A under B
	t.Log(makeHelloRequest(t, apiB, 200, vkA.ID()))
	t.Log(makeJoinRequest(t, apiB, 202, vkA.ID(), vkA.Height(), vkAAddr.String(), true))

	// start to send heartbeats A --> B
	aHBDone, aHBErr := sendVKHeartbeats(apiB, 1*time.Second, vkA.ID())

	time.Sleep(7 * time.Second)

	close(aHBDone)

	// TODO query the VK directly for a list of its children

	select {
	case e := <-aHBErr:
		// an error occurred in the heartbeater
		t.Fatalf("response code %d with response %s", e.Code, e.Body.String())
	default:
		// the heartbeater worked as intended
	}

}

// Send VK heartbeats (every sleep duration) on behalf of the cID until any value arrives on the returned channel or a heartbeat fails.
// The caller must close the done channel
func sendVKHeartbeats(targetAPI humatest.TestAPI, sleep time.Duration, cID uint64) (done chan bool, errResp chan *httptest.ResponseRecorder) {
	// create the channel
	done, errResp = make(chan bool), make(chan *httptest.ResponseRecorder)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(sleep):
				// submit a heartbeat
				resp := targetAPI.Post(orv.EP_VK_HEARTBEAT, map[string]any{
					"id": cID,
				})
				if resp.Code != 200 {
					errResp <- resp
					return
				}

			}
		}
	}()

	return

}

// Tests that VKs properly prune out leaves that do not register at least one service within a short span AND that
// services that fail to heartbeat are properly pruned out (without pruning out correctly heartbeating services).
//
// Spins up one VK and two leaves (A and B). Both leaves should successfully HELLO -> JOIN. Leaf B then REGISTERs two services and begins heartbeating them.
// Leaf A should be pruned after a short delay, as it did not register any services.
// Leaf B stops heartbeating one service. After a short detail, only that service should be pruned.
//
// By the end, the VK should have a single child (leaf B) and a single service (leaf B's service that is still sending heartbeats).
func TestLeafNoRegisterNoHeartbeat(t *testing.T) {
	slt := SuppressedLogTest{t}
	// spawn the huma test API
	_, api := humatest.New(slt)

	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}
	// spawn a VK
	vk, err := orv.NewVaultKeeper(1, vkAddr, orv.SetHumaAPI(api))
	if err != nil {
		t.Fatal("failed to construct VK: ", err)
	}
	// start the VK
	if err := vk.Start(); err != nil {
		t.Fatal("failed to start VK: ", err)
	}
	t.Cleanup(vk.Terminate)

	// issue a status request after a brief start up window
	time.Sleep(500 * time.Millisecond)
	{
		resp := api.Get(orv.EP_STATUS)
		if resp.Code != 200 {
			t.Fatal("valid status request failed: " + ErrBadResponseCode(resp.Code, 200))
		}
	}

	// spin up the first leaf
	var leafA struct {
		id           uint64
		serviceName  string
		serviceAddr  netip.AddrPort
		serviceStale time.Duration
	} = struct {
		id           uint64
		serviceName  string
		serviceAddr  netip.AddrPort
		serviceStale time.Duration
	}{
		id:          100,
		serviceName: "testServiceA",
	}

	leafA.serviceAddr, err = netip.ParseAddrPort("[::1]:8091")
	if err != nil {
		t.Fatal(err)
	}
	leafA.serviceStale, err = time.ParseDuration("3s")
	if err != nil {
		t.Fatal(err)
	}

	// introduce and join leaf A
	makeHelloRequest(t, api, 200, leafA.id)
	makeJoinRequest(t, api, 202, leafA.id, 0, "", false)
	// register the service
	makeRegisterRequest(t, api, 202, leafA.id, leafA.serviceName, leafA.serviceAddr, leafA.serviceStale)
	// make a status request to check for the service
	{
		resp := api.Get(orv.EP_STATUS)
		if resp.Code != 200 {
			t.Fatal("valid status request failed: " + ErrBadResponseCode(resp.Code, 200))
		}
		fmt.Println(resp.Body.String())
	}

	time.Sleep(6 * time.Second)

	// TODO

}

// Tests that VKs can successfully take each other on as children and that two, equal-height, root VKs can successfully merge.
//
// Three VKs are created: A, B, and C.
// A and B are given starting heights of 1.
// C joins under B.
// B then sends a MERGE to A, which A should accept.
// Upon receiving MERGE_ACCEPT, B must increment its height to 2 and send an INCR to C, which should increment its height to 1.
func TestVKJoinMerge(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}
