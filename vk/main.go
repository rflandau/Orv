/*
VaultKeeper instance to showcases how the library would be used.

Spins up a couple VKs and connects them together.
They will automatically heartbeat and retain the connection while alive.

Companion to the leaf implementation in leaf/main.py.
*/
package main

import (
	"fmt"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"os"
	"os/signal"

	"github.com/rs/zerolog"
)

func main() {
	var (
		pvkid uint64 = 1
		cvkid uint64 = 2
	)

	// spawn and start the parent VK, with options
	addr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		panic(err)
	}

	parentLogger := zerolog.New(os.Stdout).Output(os.Stdout).Level(zerolog.ErrorLevel)

	pvk, err := orv.NewVaultKeeper(pvkid, addr, orv.Height(1), orv.SetLogger(&parentLogger))
	if err != nil {
		panic(err)
	}
	if err := pvk.Start(); err != nil {
		panic(err)
	}
	defer pvk.Terminate()

	// spawn the child VK
	cvkAddr, err := netip.ParseAddrPort("[::1]:8081")
	if err != nil {
		panic(err)
	}
	cvk, err := orv.NewVaultKeeper(cvkid, cvkAddr)
	if err != nil {
		panic(err)
	}
	if err := cvk.Start(); err != nil {
		panic(err)
	}
	defer cvk.Terminate()

	// have the child greet and join the parent
	if resp, err := cvk.Hello(pvk.AddrPort().String()); resp.StatusCode() != orv.EXPECTED_STATUS_HELLO || err != nil {
		panic(fmt.Sprintf("failed to greet parent (status code: %d, error: %v)", resp.StatusCode(), err))
	}
	if err := cvk.Join(pvk.AddrPort().String()); err != nil {
		panic(err)
	}

	// submit a status request to the parent
	httpResp, statusAck, err := orv.Status("http://" + pvk.AddrPort().String())
	if err != nil {
		panic(err)
	}
	defer httpResp.Body.Close()
	fmt.Printf("status response code: %d\nunpacked body: %#v\n", httpResp.StatusCode(), statusAck.Body)

	fmt.Println("Send a SIGINT to kill the program")

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt)
	<-done

	fmt.Println("SIGINT captured. Cleaning up....")
}
