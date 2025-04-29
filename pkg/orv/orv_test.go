package orv_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"resty.dev/v3"
)

//#region request helper functions and structs

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

const (
	HelloSuccessCode    int = 200
	JoinSuccessCode     int = 202
	RegisterSuccessCode int = 202
)

type leaf struct {
	id       uint64
	services map[string]struct {
		addr  string
		stale string
	}
}

// POSTs a HELLO to the endpoint embedded in the huma api.
// Only returns if the given status code was matched; Fatal if not
func makeHelloRequest(t *testing.T, targetAddr netip.AddrPort, expectedCode int, id uint64) (*resty.Response, orv.HelloResp) {
	cli := resty.New()
	unpackedResp := orv.HelloResp{}
	resp, err := cli.R().
		SetBody(orv.HelloReq{Body: struct {
			Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
		}{id}}.Body). // default request content type is JSON
		SetExpectResponseContentType(orv.CONTENT_TYPE).
		SetResult(&(unpackedResp.Body)).
		Post("http://" + targetAddr.String() + orv.EP_HELLO)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode() != expectedCode {
		t.Fatal("hello request failed: " + ErrBadResponseCode(resp.StatusCode(), expectedCode))
	}
	return resp, unpackedResp
}

// POSTs a JOIN to the endpoint embedded in the huma api.
// Only returns if the given status code was matched; Fatal if not
func makeJoinRequest(t *testing.T, targetAPI humatest.TestAPI, expectedCode int, id uint64, height uint16, vkaddr string, isvk bool) (resp *httptest.ResponseRecorder) {
	t.Helper()
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
func makeRegisterRequest(t *testing.T, api humatest.TestAPI, expectedCode int, id uint64, sn string, apStr, staleStr string) (resp *httptest.ResponseRecorder) {
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
				Address: apStr,
				Stale:   staleStr,
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

// Send service heartbeats (at given frequency) on behalf of the cID until any value arrives on the returned channel or a heartbeat fails.
// The caller must close the done channel.
func sendServiceHeartbeats(targetAPI humatest.TestAPI, frequency time.Duration, cID uint64, services []string) (done chan bool, errResp chan *httptest.ResponseRecorder) {
	// create the channel
	done, errResp = make(chan bool), make(chan *httptest.ResponseRecorder)

	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(frequency):
				// submit a heartbeat
				resp := targetAPI.Post(orv.EP_SERVICE_HEARTBEAT, map[string]any{
					"id":       cID,
					"services": services,
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

// Checks if an error is currently waiting on the given error response channel and fails the test if it is.
func checkHeartbeatError(t *testing.T, errResp chan *httptest.ResponseRecorder) {
	select {
	case e := <-errResp:
		t.Fatalf("response code %d with response %s", e.Code, e.Body.String())
	default:
	}
}

//#endregion

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

func StartVKListener(t *testing.T, api huma.API, vkid uint64) (*orv.VaultKeeper, netip.AddrPort) {
	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}

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
	// spawn the huma test API
	_, api := humatest.New(t)

	vk, vkAddr := StartVKListener(t, api, 1)

	time.Sleep(1 * time.Second) // give the VK time to start up

	t.Cleanup(vk.Terminate)

	// submit a valid HELLO
	makeHelloRequest(t, vkAddr, 200, 2)

	// submit a valid JOIN
	makeJoinRequest(t, api, 202, 2, 0, "", false)

	// submit a HELLO with an invalid ID
	makeHelloRequest(t, vkAddr, 400, 0)

	// submit a JOIN with an invalid ID and height
	makeJoinRequest(t, api, 400, 0, 0, "", false)

	// submit a JOIN with same ID
	makeJoinRequest(t, api, 409, 2, 0, "", false)

	// submit a REGISTER with an invalid ID
	makeRegisterRequest(t, api, 400, 3, "Good advice generator - Just drink Milk instead of Coffee", vkAddr.String(), time.Second.String())

	// submit a REGISTER for an unjoined ID
	makeHelloRequest(t, vkAddr, 200, 3)
	makeRegisterRequest(t, api, 400, 3, "Very good Coffee Maker - I might just beat Starbucks", vkAddr.String(), time.Second.String())

	fmt.Println("ok")

	// submit a valid REGISTER for vk
	makeHelloRequest(t, vkAddr, 4, 100)
	makeJoinRequest(t, api, 202, 4, 1, "", true)
	makeRegisterRequest(t, api, 4, 100, "Horrible Coffee Maker", vkAddr.String(), time.Second.String())

	// submit a valid REGISTER for vk and check if multiple services are allowed
	makeRegisterRequest(t, api, 200, 4, "Tea Maker - makes sense why I make horrible Coffee", vkAddr.String(), time.Second.String())

	// submit a invalid REGISTER for vk
	makeRegisterRequest(t, api, 400, 4, "", vkAddr.String(), time.Second.String())

	fmt.Println("ok")

	// submit a valid HELLO to the same ID as VK
	makeHelloRequest(t, vkAddr, 400, 1)
	makeJoinRequest(t, api, 400, 1, 0, "", false)
	makeRegisterRequest(t, api, 400, 1, "Flopped Coffee", vkAddr.String(), time.Second.String())

}

// Tests that a single VK can support multiple leaves and multiple services (some duplicates) on each leaf simultaneously.
// Each leaf will HELLO -> JOIN and then submit multiple REGISTERS. Each service will need to send heartbeats to the VK.
// After a short delay, the test checks if the VK still believe that all services are active.
func TestMultiLeafMultiService(t *testing.T) {
	// wrap the test in a no-op for huma
	nt := SuppressedLogTest{t}

	// spawn the huma test api
	_, api := humatest.New(nt)

	addr, err := netip.ParseAddrPort("[::1]:42069")
	if err != nil {
		t.Fatal(err)
	}

	vk, err := orv.NewVaultKeeper(1, addr, orv.SetHumaAPI(api))
	if err != nil {
		t.Fatal("failed to construct parent VK: ", err)
	}
	if err := vk.Start(); err != nil {
		t.Fatal("failed to start parent VK: ", err)
	}
	t.Cleanup(vk.Terminate)

	// populate the leaf information
	lA, lB, lC := leaf{
		id: 100, services: map[string]struct {
			addr  string
			stale string
		}{
			"ssh":  {addr: "127.0.0.1:6001", stale: "1s"},
			"http": {addr: "127.0.0.1:6002", stale: "1s"}}},
		leaf{id: 200, services: map[string]struct {
			addr  string
			stale string
		}{
			"ssh": {addr: "127.0.0.1:6011", stale: "1s"},
			"dns": {addr: "127.0.0.1:6012", stale: "1s200ms"}}},
		leaf{id: 300, services: map[string]struct {
			addr  string
			stale string
		}{
			"some longish service name": {addr: "127.0.0.1:6021", stale: "1s"},
			"who even knows, man":       {addr: "127.0.0.1:6022", stale: "800ms"}}}
	// register each leaf under the VK and start heartbeats for it
	makeHelloRequest(t, addr, orv.EXPECTED_STATUS_HELLO, lA.id)
	makeJoinRequest(t, api, orv.EXPECTED_STATUS_JOIN, lA.id, 0, "", false)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lA.id, "ssh", lA.services["ssh"].addr, lA.services["ssh"].stale)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lA.id, "http", lA.services["http"].addr, lA.services["http"].stale)
	makeHelloRequest(t, addr, orv.EXPECTED_STATUS_HELLO, lB.id)
	makeJoinRequest(t, api, orv.EXPECTED_STATUS_JOIN, lB.id, 0, "", false)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lB.id, "ssh", lB.services["ssh"].addr, lB.services["ssh"].stale)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lB.id, "dns", lB.services["dns"].addr, lB.services["dns"].stale)
	lADone, lAErr := sendServiceHeartbeats(api, 300*time.Millisecond, lA.id, slices.Collect(maps.Keys(lA.services)))
	lBDone, lBErr := sendServiceHeartbeats(api, 300*time.Millisecond, lB.id, slices.Collect(maps.Keys(lB.services)))
	makeHelloRequest(t, addr, orv.EXPECTED_STATUS_HELLO, lC.id)
	makeJoinRequest(t, api, orv.EXPECTED_STATUS_JOIN, lC.id, 0, "", false)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lC.id, "some longish service name", lC.services["some longish service name"].addr, lC.services["some longish service name"].stale)
	makeRegisterRequest(t, api, orv.EXPECTED_STATUS_REGISTER, lC.id, "who even knows, man", lC.services["who even knows, man"].addr, lC.services["who even knows, man"].stale)
	lCDone, lCErr := sendServiceHeartbeats(api, 300*time.Millisecond, lC.id, slices.Collect(maps.Keys(lC.services)))
	t.Cleanup(func() { close(lADone) })
	t.Cleanup(func() { close(lBDone) })
	t.Cleanup(func() { close(lCDone) })

	// allow leaves to expire if they are going
	time.Sleep(2 * time.Second)
	select {
	case e := <-lAErr:
		t.Fatalf("response code %d with response %s", e.Code, e.Body.String())
	default:
	}
	select {
	case e := <-lBErr:
		t.Fatalf("response code %d with response %s", e.Code, e.Body.String())
	default:
	}
	select {
	case e := <-lCErr:
		t.Fatalf("response code %d with response %s", e.Code, e.Body.String())
	default:
	}

	// check that the VK believes it has the correct number of children, services, and service providers
	snap := vk.ChildrenSnapshot()
	if len(snap.CVKs) > 0 {
		t.Fatal("expected to find zero cVKs, found ", len(snap.CVKs))
	}
	if len(snap.Leaves) != 3 {
		t.Fatal("expected to find 3 leaves, found ", len(snap.Leaves))
	}
	if sshProviders := snap.Services["ssh"]; len(sshProviders) != 2 {
		t.Fatal("expected to find 2 providers of the ssh service, found ", len(sshProviders))
	}
	if providerCount := len(snap.Services["http"]); providerCount != 1 {
		t.Fatal("expected to find 1 providers of the http service, found ", providerCount)
	}
	if providerCount := len(snap.Services["dns"]); providerCount != 1 {
		t.Fatal("expected to find 1 providers of the dns service, found ", providerCount)
	}

}

// Tests that a VK will automatically prune out individual services that do not heartbeat and all services learned by a child VK when the cVK does not heartbeat.
func TestAutoPrune(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that we can build a small vault without merging
// Composes a tree of the form LeafA --> VKA --> VKB <-- LeafB, including consistent heartbeats for leaves (and the self-managing heartbeats inherent to VKs).
// VKB and VKA do not merge; instead VKB is spawned with a Dragon's Hoard of 1 and VKA joins as a child.
func TestSmallVault(t *testing.T) {
	vkAAddr, err := netip.ParseAddrPort("[::1]:8090")
	if err != nil {
		t.Fatal(err)
	}
	vkBAddr, err := netip.ParseAddrPort("[::1]:9000")
	if err != nil {
		t.Fatal(err)
	}

	vkA, err := orv.NewVaultKeeper(1, vkAAddr)
	if err != nil {
		t.Fatal(err)
	}
	if err := vkA.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vkA.Terminate)
	vkB, err := orv.NewVaultKeeper(2, vkBAddr, orv.Height(1))
	if err != nil {
		t.Fatal(err)
	}
	if err := vkB.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(vkB.Terminate)
	// Join A under B
	if _, err := vkA.Hello(vkB.AddrPort().String()); err != nil {
		t.Fatal(err)
	}
	if err := vkA.Join(vkB.AddrPort().String()); err != nil {
		t.Fatal(err)
	}

	// join leaf B under B
	lB := leaf{id: 200, services: map[string]struct {
		addr  string
		stale string
	}{"temp": {"2.2.2.2:99", "2s"}}}
	makeHelloRequest(t, vkBAddr, orv.EXPECTED_STATUS_HELLO, lB.id)
	makeJoinRequest(t, apiB, orv.EXPECTED_STATUS_JOIN, lB.id, 0, "", false)
	makeRegisterRequest(t, apiB, orv.EXPECTED_STATUS_REGISTER, lB.id, "temp", lB.services["temp"].addr, lB.services["temp"].stale)
	lBDone, lBErr := sendServiceHeartbeats(apiB, 500*time.Millisecond, lB.id, []string{"temp"})
	t.Cleanup(func() { close(lBDone) })
	// join leaf A under A
	lA := leaf{id: 100, services: map[string]struct {
		addr  string
		stale string
	}{"temp": {"1.1.1.1:99", "1s700ms"}}}
	makeHelloRequest(t, vkAAddr, orv.EXPECTED_STATUS_HELLO, lA.id)
	makeJoinRequest(t, apiA, orv.EXPECTED_STATUS_JOIN, lA.id, 0, "", false)
	makeRegisterRequest(t, apiA, orv.EXPECTED_STATUS_REGISTER, lA.id, "temp", lA.services["temp"].addr, lA.services["temp"].stale)
	lADone, lAErr := sendServiceHeartbeats(apiA, 500*time.Millisecond, lA.id, []string{"temp"})
	t.Cleanup(func() { close(lADone) })

	time.Sleep(6 * time.Second)

	// check for heartbeater errors
	checkHeartbeatError(t, lBErr)
	checkHeartbeatError(t, lAErr)

	// check that B still considers A and LeafB to be its children and that it is aware that leafB offers 1 service.
	snap := vkB.ChildrenSnapshot()
	if _, exists := snap.CVKs[vkA.ID()]; !exists {
		t.Fatal("cVK A was pruned from B despite no heartbeater errors. Snapshot:", snap)
	}

	// check that A's parent is correctly set to B
	if p := vkA.Parent(); p.Id != vkB.ID() || p.Addr != vkBAddr {
		t.Fatal("A does not believe B is its parent")
	}

	// TODO

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
	// spawn the huma test API
	_, api := humatest.New(t)

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
	leafA := leaf{
		id: 100,
		services: map[string]struct {
			addr  string
			stale string
		}{
			"testServiceA": {"[::1]:8091", "3s"},
		},
	}
	// introduce and join leaf A
	makeHelloRequest(t, vkAddr, 200, leafA.id)
	makeJoinRequest(t, api, 202, leafA.id, 0, "", false)
	// register the service
	makeRegisterRequest(t, api, 202, leafA.id, "testServiceA", leafA.services["testServiceA"].addr, leafA.services["testServiceA"].stale)
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
