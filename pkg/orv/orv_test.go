package orv_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"strconv"
	"testing"
	"time"
)

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

// Simple but important test to guarantee proper acceptance and rejection of message types to each endpoint.
// A la ClientArgs in lab3.
func TestEndpointArgs(t *testing.T) {
	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}

	// spin up vk
	var vkid uint64 = 1
	vk, err := orv.NewVaultKeeper(vkid, vkAddr)
	if err != nil {
		t.Fatal(err)
	}
	if err := vk.Start(); err != nil {
		t.Fatal(err)
	}

	time.Sleep(1 * time.Second) // give the VK time to start up

	respStatus, resp, err := getResponse("[::1]", 8080, orv.HELLO, orv.HelloReq{Body: struct {
		Id uint64 "json:\"id\" required:\"true\" example:\"718926735\" doc:\"unique identifier for this specific node\""
	}{Id: 2}})
	if err != nil {
		t.Fatalf("valid hello failed (code: %d, err: %v)", respStatus, err)
	}
	if resp == nil {
		t.Fatal("hello response failed:", resp)
	}
	t.Log("hello response:", resp)

	join_req := orv.JoinReq{}
	join_req.Body.Id = 2
	join_req.Body.Height = 5

	respStatus, resp, err = getResponse("[::1]", 8080, "/join", join_req)
	if err != nil {
		t.Fatal("join failed:", err)
	}
	if resp == nil {
		t.Fatal("join response failed:", resp)
	}
	fmt.Println("join response:", respStatus)

	fmt.Println("Ok")

	// // Hello call
	// resp, err := http.Post("http://[::1]:8080/hello", "application/json", bytes.NewReader(jsonBytes))
	// if err != nil {
	// 	t.Fatal("Failed to make request:", err)
	// }

	// body, err := io.ReadAll(resp.Body)
	// if err != nil {
	// 	t.Fatal("Failed read body:", err)
	// }

	// fmt.Println("Body -", body)

	/*
		Aye. We probably need to implement vk.Start and vk.Stop before the tests will work properly
		but then you should just be able to call vk.Start and, in the same thread (assuming we make it non-blocking)
		start issuing requests to the endpoint it is listening on Now
	*/

	// send a bunch of garbage and out of order requests (ex joins before hello)

	//http.Post()

	/*
		fmt.Print("Checking error responses by list endpoint ... ")
		lr := ListResponse{}

		p, e := json.Marshal(DirectoryRequest{Directory: "", SeqNumber: 0})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err := getResponse("localhost", ctrl.basePort, "/list", p, &lr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("List endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(DirectoryRequest{Directory: "dir", SeqNumber: 1})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/list", p, &lr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("List endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(DirectoryRequest{Directory: "/dir:name", SeqNumber: 2})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/list", p, &lr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("List endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(DirectoryRequest{Directory: "/dir", SeqNumber: 3})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/list", p, &lr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("List endpoint accepted non-existent directory or returned incorrect error type")
		}

		fmt.Println("ok")

		fmt.Print("Checking error responses by get_metadata endpoint ... ")
		mr := MetadataResponse{}

		p, e = json.Marshal(KeyRequest{Directory: "", Key: "key", SeqNumber: 4})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get-metadata endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "", SeqNumber: 5})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get-metadata endpoint accepted empty key or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "dir", Key: "key", SeqNumber: 6})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get-metadata endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir:name", Key: "key", SeqNumber: 7})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get-metadata endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "key", SeqNumber: 8})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Get-metadata endpoint accepted non-existent directory or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/", Key: "key", SeqNumber: 9})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get_metadata", p, &mr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != KeyNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Get-metadata endpoint accepted non-existent key or returned incorrect error type")
		}

		fmt.Println("ok")

		fmt.Print("Checking error responses by get endpoint ... ")
		kvm := KeyValueMessage{}

		p, e = json.Marshal(KeyRequest{Directory: "", Key: "key", SeqNumber: 10})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "dir", Key: "key", SeqNumber: 11})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir:name", Key: "key", SeqNumber: 12})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Get endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "key", SeqNumber: 13})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Get endpoint accepted non-existent directory or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/", Key: "key", SeqNumber: 14})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/get", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != KeyNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Get endpoint accepted non-existent key or returned incorrect error type")
		}

		fmt.Println("ok")

		fmt.Print("Checking error responses by set endpoint ... ")
		ksr := KeySuccessResponse{}

		p, e = json.Marshal(KeyValueMessage{Directory: "", Key: "key", Value: "abcd", SeqNumber: 15})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/set", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Set endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyValueMessage{Directory: "dir", Key: "key", Value: "abcd", SeqNumber: 16})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/set", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Set endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(KeyValueMessage{Directory: "/dir:name", Key: "key", Value: "abcd", SeqNumber: 17})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/set", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Set endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(KeyValueMessage{Directory: "/dir", Key: "key", Value: "abcd", SeqNumber: 18})
		if e != nil {
			t.Fatal("Error encoding directory request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/set", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Set endpoint accepted non-existent directory or returned incorrect error type")
		}

		fmt.Println("ok")

		fmt.Print("Checking error responses by create endpoint ... ")

		p, e = json.Marshal(KeyRequest{Directory: "", Key: "key", SeqNumber: 19})
		if e != nil {
			t.Fatal("Error encoding create request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/create", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Create endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "dir", Key: "key", SeqNumber: 20})
		if e != nil {
			t.Fatal("Error encoding create request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/create", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Create endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir:name", Key: "key", SeqNumber: 21})
		if e != nil {
			t.Fatal("Error encoding create request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/create", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Create endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "key", SeqNumber: 22})
		if e != nil {
			t.Fatal("Error encoding create request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/create", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Create endpoint accepted non-existent directory or returned incorrect error type")
		}

		fmt.Println("ok")

		fmt.Print("Checking error responses by delete endpoint ... ")

		p, e = json.Marshal(KeyRequest{Directory: "", Key: "key", SeqNumber: 23})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Delete endpoint accepted empty directory request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "dir", Key: "key", SeqNumber: 24})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Delete endpoint accepted directory with no leading / or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir:name", Key: "key", SeqNumber: 25})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Delete endpoint accepted directory with : or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "", SeqNumber: 26})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != InvalidError || rc != http.StatusBadRequest {
			t.Fatal("Delete endpoint accepted empty key request or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/dir", Key: "key", SeqNumber: 27})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &ksr)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != DirNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Delete endpoint accepted non-existent directory or returned incorrect error type")
		}

		p, e = json.Marshal(KeyRequest{Directory: "/", Key: "key", SeqNumber: 28})
		if e != nil {
			t.Fatal("Error encoding delete request: ", e)
		}
		rc, er, err = getResponse("localhost", ctrl.basePort, "/delete", p, &kvm)
		if err != nil {
			t.Fatal(err)
		}
		if er == nil || er.ErrorType != KeyNotFoundError || rc != http.StatusNotFound {
			t.Fatal("Delete endpoint accepted non-existent key or returned incorrect error type")
		}

		fmt.Println("ok")
	*/

}

// Tests that a single VK can support multiple leaves and multiple services on each leaf simultaneously.
// Each leaf will HELLO -> JOIN and then submit multiple REGISTERS. Each service will need to send heartbeats to the VK.
// After a short detail, the test checks if the VK still believe that all services are active.
func TestMultiLeafMultiService(t *testing.T) {
	// TODO
	t.Fatal("NYI")
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
	vkAddr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		t.Fatal(err)
	}
	// spawn a VK
	vk, err := orv.NewVaultKeeper(1, vkAddr)
	if err != nil {
		t.Fatal("failed to construct VK: ", err)
	}
	// start the VK
	if err := vk.Start(); err != nil {
		t.Fatal("failed to start VK: ", err)
	}
	t.Cleanup(vk.Terminate)

	// issue a status request after a brief start up window
	time.Sleep(1 * time.Second)

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
