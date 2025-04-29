/*
Tests for the Orv package
*/
package orv_test

import (
	"fmt"
	"io"
	"maps"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"slices"
	"testing"
	"time"

	"resty.dev/v3"
)

//#region request helper functions and structs

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

// HELLOs and JOINs under the given VK, calling Fatal if any step fails.
// REGISTERs each service defined in the leaf.
// Does not start a heartbeater for any service.
func (l *leaf) JoinVault(t *testing.T, parent *orv.VaultKeeper) {
	makeHelloRequest(t, parent.AddrPort(), orv.EXPECTED_STATUS_HELLO, l.id)
	makeJoinRequest(t, parent.AddrPort(), orv.EXPECTED_STATUS_JOIN, l.id, 0, "", false)
	for k, v := range l.services {
		makeRegisterRequest(t, parent.AddrPort(), orv.EXPECTED_STATUS_REGISTER, l.id, k, v.addr, v.stale)
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
func makeJoinRequest(t *testing.T, targetAddr netip.AddrPort, expectedCode int, id uint64, height uint16, vkaddr string, isvk bool) (*resty.Response, orv.JoinAcceptResp) {
	t.Helper()
	cli := resty.New()
	unpackedResp := orv.JoinAcceptResp{}
	resp, err := cli.R().
		SetBody(orv.JoinReq{Body: struct {
			Id     uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
			Height uint16 "json:\"height,omitempty\" dependentRequired:\"is-vk\" example:\"3\" doc:\"height of the vk attempting to join the vault\""
			VKAddr string "json:\"vk-addr,omitempty\" dependentRequired:\"is-vk\" example:\"174.1.3.4:8080\" doc:\"address of the listening VK service that can receive INCRs\""
			IsVK   bool   "json:\"is-vk,omitempty\" example:\"false\" doc:\"is this node a VaultKeeper or a leaf? If true, height and VKAddr are required\""
		}{id, height, vkaddr, isvk}}.Body). // default request content type is JSON
		SetExpectResponseContentType(orv.CONTENT_TYPE).
		SetResult(&(unpackedResp.Body)).
		Post("http://" + targetAddr.String() + orv.EP_JOIN)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode() != expectedCode {
		t.Fatal("Join request failed: " + ErrBadResponseCode(resp.StatusCode(), expectedCode))
	}
	return resp, unpackedResp
}

// POSTs a REGISTER to the endpoint embedded in the huma api, registering a new service under the given id.
// Only returns if the given status code was matched; Fatal if not
func makeRegisterRequest(t *testing.T, targetAddr netip.AddrPort, expectedCode int, id uint64, sn string, apStr, staleStr string) (*resty.Response, orv.RegisterAcceptResp) {
	t.Helper()
	cli := resty.New()
	unpackedResp := orv.RegisterAcceptResp{}
	resp, err := cli.R().
		SetBody(orv.RegisterReq{Body: struct {
			Id      uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
			Service string "json:\"service\" required:\"true\" example:\"SSH\" doc:\"the name of the service to be registered\""
			Address string "json:\"address\" required:\"true\" example:\"172.1.1.54:22\" doc:\"the address the service is bound to. Only populated from leaf to parent.\""
			Stale   string "json:\"stale\" example:\"1m5s45ms\" doc:\"after how much time without a heartbeat is this service eligible for pruning\""
		}{id, sn, apStr, staleStr}}.Body). // default request content type is JSON
		SetExpectResponseContentType(orv.CONTENT_TYPE).
		SetResult(&(unpackedResp.Body)).
		Post("http://" + targetAddr.String() + orv.EP_REGISTER)
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode() != expectedCode {
		t.Fatal("Register request failed: " + ErrBadResponseCode(resp.StatusCode(), expectedCode))
	}
	return resp, unpackedResp
}

// Send service heartbeats (at given frequency) on behalf of the cID until any value arrives on the returned channel or a heartbeat fails.
// The caller must close the done channel.
func sendServiceHeartbeats(targetAddr netip.AddrPort, frequency time.Duration, cID uint64, services []string) (done chan bool, errResp chan *resty.Response) {
	// create the channel
	done, errResp = make(chan bool), make(chan *resty.Response)

	cli := resty.New()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(frequency):
				// submit a heartbeat
				resp, err := cli.R().
					SetBody(orv.ServiceHeartbeatReq{Body: struct {
						Id       uint64   "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier of the child VK being refreshed\""
						Services []string "json:\"services\" required:\"true\" example:\"[\\\"serviceA\\\", \\\"serviceB\\\"]\" doc:\"the name of the services to refresh\""
					}{cID, services}}.Body). // default request content type is JSON
					SetExpectResponseContentType(orv.CONTENT_TYPE).
					Post("http://" + targetAddr.String() + orv.EP_SERVICE_HEARTBEAT)
				if err != nil || resp.StatusCode() != orv.EXPECTED_STATUS_SERVICE_HEARTBEAT {
					errResp <- resp
				}
			}
		}
	}()

	return
}

// Checks if an error is currently waiting on the given error response channel and fails the test if it is.
func checkHeartbeatError(t *testing.T, errResp chan *resty.Response) {
	select {
	case e := <-errResp:
		b, err := io.ReadAll(e.Body)
		if err != nil {
			t.Fatal(err)
		}
		t.Fatalf("response code %d with response %s", e.StatusCode(), string(b))
	default:
	}
}

// Generates 3 VKs in the form A --> B --> C with C at the root.
// Returns handles to all 3.
// Calls Fatal if any step fails
func buildLineVault(t *testing.T) (A, B, C *orv.VaultKeeper) {
	vkAAddr, err := netip.ParseAddrPort("[::1]:7001")
	if err != nil {
		t.Fatal("failed to parse addrport: ", err)
	}
	vkA, err := orv.NewVaultKeeper(1, vkAAddr)
	if err != nil {
		t.Fatal("failed to create VK: ", err)
	}
	if err := vkA.Start(); err != nil {
		t.Fatal("failed to start VK: ", err)
	}
	t.Cleanup(vkA.Terminate)

	vkBAddr, err := netip.ParseAddrPort("[::1]:7002")
	if err != nil {
		t.Fatal("failed to parse addrport: ", err)
	}
	vkB, err := orv.NewVaultKeeper(2, vkBAddr, orv.Height(1))
	if err != nil {
		t.Fatal("failed to create VK: ", err)
	}
	if err := vkB.Start(); err != nil {
		t.Fatal("failed to start VK: ", err)
	}
	t.Cleanup(vkB.Terminate)

	vkCAddr, err := netip.ParseAddrPort("[::1]:7003")
	if err != nil {
		t.Fatal("failed to parse addrport: ", err)
	}
	vkC, err := orv.NewVaultKeeper(3, vkCAddr, orv.Height(2))
	if err != nil {
		t.Fatal("failed to create VK: ", err)
	}
	if err := vkC.Start(); err != nil {
		t.Fatal("failed to start VK: ", err)
	}
	t.Cleanup(vkC.Terminate)

	// Join A under B
	if resp, err := vkA.Hello(vkBAddr.String()); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode() != orv.EXPECTED_STATUS_HELLO {
		t.Fatalf("bad status code (got %d, expected %d)", resp.StatusCode(), orv.EXPECTED_STATUS_HELLO)
	}
	if err := vkA.Join(vkBAddr.String()); err != nil {
		t.Fatal(err)
	}

	// Join B under C
	if resp, err := vkB.Hello(vkCAddr.String()); err != nil {
		t.Fatal(err)
	} else if resp.StatusCode() != orv.EXPECTED_STATUS_HELLO {
		t.Fatalf("bad status code (got %d, expected %d)", resp.StatusCode(), orv.EXPECTED_STATUS_HELLO)
	}
	if err := vkB.Join(vkCAddr.String()); err != nil {
		t.Fatal(err)
	}

	return vkA, vkB, vkC
}

//#endregion

//#region testing error messages

func ErrBadResponseCode(got, expected int) string {
	return fmt.Sprintf("incorrect response code (got: %d, expected %d)", got, expected)
}

//#endregion

// Simple but important test to guarantee proper acceptance and rejection of message types to each endpoint.
// A la ClientArgs in lab3.
func TestEndpointArgs(t *testing.T) {
	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}

	vk, err := orv.NewVaultKeeper(1, vkAddr)
	if err != nil {
		t.Fatal(err)
	}

	if err := vk.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second) // give the VK time to start up

	t.Cleanup(vk.Terminate)

	lA, lB, lC, lD, lE := leaf{
		id: 100, services: map[string]struct {
			addr  string
			stale string
		}{
			"Good advice generator - Just drink Milk instead of Coffee": {addr: "127.0.0.1:5001", stale: "1s"},
		}},
		leaf{id: 200, services: map[string]struct {
			addr  string
			stale string
		}{
			"Very good Coffee Maker - I might just beat Starbucks": {addr: "127.0.0.1:5011", stale: "1s"},
		}},
		leaf{id: 300, services: map[string]struct {
			addr  string
			stale string
		}{
			"Horrible Coffee Maker": {addr: "127.0.0.1:5021", stale: "1s"},
		}},
		leaf{id: 300, services: map[string]struct {
			addr  string
			stale string
		}{
			"Flopped Coffee": {addr: "127.0.0.1:5051", stale: "1s"},
		}},
		leaf{id: 1, services: map[string]struct {
			addr  string
			stale string
		}{
			"Tea Maker - makes sense why I make horrible Coffee": {addr: "127.0.0.1:5031", stale: "1s"},
		}}

	// submit a valid HELLO
	makeHelloRequest(t, vkAddr, 200, 2)

	// submit a valid JOIN
	makeJoinRequest(t, vkAddr, 202, 2, 0, "", false)

	// submit a HELLO with an invalid ID
	makeHelloRequest(t, vkAddr, 400, 0)

	// submit a JOIN with an invalid ID and height
	makeJoinRequest(t, vkAddr, 400, 0, 0, "", false)

	// submit a JOIN with same ID
	makeJoinRequest(t, vkAddr, 202, 2, 0, "", false)
	fmt.Println("ok")

	// submit a REGISTER with an invalid ID
	makeRegisterRequest(t, vkAddr, 500, lA.id, "Good advice generator - Just drink Milk instead of Coffee", lA.services["Good advice generator - Just drink Milk instead of Coffee"].addr, lA.services["Good advice generator - Just drink Milk instead of Coffee"].stale)

	// submit a REGISTER for an unjoined ID
	makeHelloRequest(t, vkAddr, 200, lB.id)
	makeRegisterRequest(t, vkAddr, 500, lB.id, "Very good Coffee Maker - I might just beat Starbucks", lB.services["Very good Coffee Maker - I might just beat Starbucks"].addr, lB.services["Very good Coffee Maker - I might just beat Starbucks"].stale)
	fmt.Println("ok")

	// submit a valid REGISTER for leaf
	makeHelloRequest(t, vkAddr, 200, lC.id)
	makeJoinRequest(t, vkAddr, 202, lC.id, 0, "", false)
	makeRegisterRequest(t, vkAddr, 202, lC.id, "Horrible Coffee Maker", lC.services["Horrible Coffee Maker"].addr, lC.services["Horrible Coffee Maker"].stale)

	// submit a valid REGISTER for leaf and check if multiple services are allowed
	makeRegisterRequest(t, vkAddr, 202, lD.id, "Flopped Coffee", lD.services["Flopped Coffee"].addr, lD.services["Flopped Coffee"].stale)

	// submit a invalid REGISTER for leaf
	makeRegisterRequest(t, vkAddr, 400, lC.id, "", "127.0.0.1:5061", "1s")

	fmt.Println("ok")

	// submit a valid HELLO to the same ID as VK
	makeHelloRequest(t, vkAddr, 400, 1)
	makeJoinRequest(t, vkAddr, 400, 1, 0, "", false)
	makeRegisterRequest(t, vkAddr, 400, 1, "Tea Maker - makes sense why I make horrible Coffee", lE.services["Tea Maker - makes sense why I make horrible Coffee"].addr, lE.services["Tea Maker - makes sense why I make horrible Coffee"].stale)

}

// Tests that a single VK can support multiple leaves and multiple services (some duplicates) on each leaf simultaneously.
// Each leaf will HELLO -> JOIN and then submit multiple REGISTERS. Each service will need to send heartbeats to the VK.
// After a short delay, the test checks if the VK still believe that all services are active.
func TestMultiLeafMultiService(t *testing.T) {
	addr, err := netip.ParseAddrPort("[::1]:42069")
	if err != nil {
		t.Fatal(err)
	}

	vk, err := orv.NewVaultKeeper(1, addr)
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
	makeJoinRequest(t, addr, orv.EXPECTED_STATUS_JOIN, lA.id, 0, "", false)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lA.id, "ssh", lA.services["ssh"].addr, lA.services["ssh"].stale)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lA.id, "http", lA.services["http"].addr, lA.services["http"].stale)
	makeHelloRequest(t, addr, orv.EXPECTED_STATUS_HELLO, lB.id)
	makeJoinRequest(t, addr, orv.EXPECTED_STATUS_JOIN, lB.id, 0, "", false)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lB.id, "ssh", lB.services["ssh"].addr, lB.services["ssh"].stale)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lB.id, "dns", lB.services["dns"].addr, lB.services["dns"].stale)
	lADone, lAErr := sendServiceHeartbeats(addr, 300*time.Millisecond, lA.id, slices.Collect(maps.Keys(lA.services)))
	lBDone, lBErr := sendServiceHeartbeats(addr, 300*time.Millisecond, lB.id, slices.Collect(maps.Keys(lB.services)))
	makeHelloRequest(t, addr, orv.EXPECTED_STATUS_HELLO, lC.id)
	makeJoinRequest(t, addr, orv.EXPECTED_STATUS_JOIN, lC.id, 0, "", false)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lC.id, "some longish service name", lC.services["some longish service name"].addr, lC.services["some longish service name"].stale)
	makeRegisterRequest(t, addr, orv.EXPECTED_STATUS_REGISTER, lC.id, "who even knows, man", lC.services["who even knows, man"].addr, lC.services["who even knows, man"].stale)
	lCDone, lCErr := sendServiceHeartbeats(addr, 300*time.Millisecond, lC.id, slices.Collect(maps.Keys(lC.services)))
	t.Cleanup(func() { close(lADone) })
	t.Cleanup(func() { close(lBDone) })
	t.Cleanup(func() { close(lCDone) })

	// allow leaves to expire if they are going
	time.Sleep(2 * time.Second)

	checkHeartbeatError(t, lAErr)
	checkHeartbeatError(t, lBErr)
	checkHeartbeatError(t, lCErr)

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

// A simple test to ensure that leaves that do not register a service are pruned after a short delay.
func TestChildlessService(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that we can build a small vault without merging.
// Composes a tree of the form LeafA --> VKA --> VKB <-- LeafB, including consistent heartbeats for leaves (and the self-managing heartbeats inherent to VKs).
// VKB and VKA do not merge; instead VKB is spawned with a Dragon's Hoard of 1 and VKA joins as a child.
func TestSmallVaultDragonsHoard(t *testing.T) {
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
	makeJoinRequest(t, vkBAddr, orv.EXPECTED_STATUS_JOIN, lB.id, 0, "", false)
	makeRegisterRequest(t, vkBAddr, orv.EXPECTED_STATUS_REGISTER, lB.id, "temp", lB.services["temp"].addr, lB.services["temp"].stale)
	lBDone, lBErr := sendServiceHeartbeats(vkBAddr, 500*time.Millisecond, lB.id, []string{"temp"})
	t.Cleanup(func() { close(lBDone) })
	// join leaf A under A
	lA := leaf{id: 100, services: map[string]struct {
		addr  string
		stale string
	}{"temp": {"1.1.1.1:99", "1s700ms"}}}
	makeHelloRequest(t, vkAAddr, orv.EXPECTED_STATUS_HELLO, lA.id)
	makeJoinRequest(t, vkAAddr, orv.EXPECTED_STATUS_JOIN, lA.id, 0, "", false)
	makeRegisterRequest(t, vkAAddr, orv.EXPECTED_STATUS_REGISTER, lA.id, "temp", lA.services["temp"].addr, lA.services["temp"].stale)
	lADone, lAErr := sendServiceHeartbeats(vkAAddr, 500*time.Millisecond, lA.id, []string{"temp"})
	t.Cleanup(func() { close(lADone) })

	time.Sleep(6 * time.Second)

	// check for heartbeater errors
	checkHeartbeatError(t, lBErr)
	checkHeartbeatError(t, lAErr)

	// check that B still considers A and LeafB to be its children and that it is aware that leafB offers 1 service.
	snapB := vkB.ChildrenSnapshot()
	if _, exists := snapB.CVKs[vkA.ID()]; !exists { // B knows A exists
		t.Fatal("cVK A was pruned from B despite no heartbeater errors. Snapshot:", snapB)
	}
	if _, exists := snapB.Leaves[lB.id]; !exists {
		t.Fatal("leaf B was pruned from B despite no heartbeater errors. Snapshot:", snapB)
	}
	// check that A's parent is correctly set to B
	if p := vkA.Parent(); p.Id != vkB.ID() || p.Addr != vkBAddr {
		t.Fatal("A does not believe B is its parent")
	}
	// check that A propagated leafA's service registration up to B
	s, exists := snapB.Services["temp"]
	if !exists {
		t.Fatal("B does not know about a service registered under A")
	}
	{ // break out scope for temp vars
		found := 0
		vkAProviderStr := fmt.Sprintf("%d(%v)", vkA.ID(), "1.1.1.1:99")
		for _, provider := range s {
			if provider == vkAProviderStr {
				found += 1
			}
		}
		// ensure that we found vkA in B's list of services for temp exactly once
		switch found {
		case 0:
			t.Fatal("vkA is not a provider of the temp service. snap:", snapB)
		case 1:
			break
		default:
			t.Fatal("vkA is a provider of the temp service more than once (", found, " times)")
		}
	}

}

// Tests that a VK will automatically prune out individual services that do not heartbeat and all services learned by a child VK when the cVK does not heartbeat.
//
// Sets up a vault similar to SmallVault, but terminates the childVK and stops heartbeating a leaf service.
//
// LeafA --> VKA --> VKB <-- LeafB
// VKB <-- LeafC
//
// VKA is taken offline, as is LeafB. By the end, the vault should only consist of VKB <-- LeafC
func TestAutoPrune(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that child VKs properly drop an unresponsive parent.
// Builds a vault:
//
// VKA --> VKB <-- VKC <-- VKD
//
// Kills VB. VKs A and C should become root after a short delay. VKD should be unaffected.
func TestUnresponsiveParent(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that we can successfully make list requests against a vault.
// Builds a small vault, registers a couple services to it at different levels, and then makes a couple List requests at different levels.
func TestListRequest(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}

// Tests that we can successfully make get requests against a vault.
// Builds a small vault, registers a couple services to it at different levels, and then checks that we can successfully query services at any level.
func TestGetRequest(t *testing.T) {
	buildLineVault(t)

	// register a leaf under C
	// TODO

	time.Sleep(1 * time.Second)
}

// Tests that VKs can successfully take each other on as children and that two, equal-height, root VKs can successfully merge.
//
// Three VKs are created: A, B, and C.
// A and B are given starting heights of 1.
// C joins under B.
// B then sends a MERGE to A, which A should accept.
// Upon receiving MERGE_ACCEPT, B must increment its height to 2 and send an INCREMENT to C, which should increment its height to 1.
/*func TestVKJoinMerge(t *testing.T) {
	// TODO
	t.Fatal("NYI")
}*/
