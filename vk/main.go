/*
VaultKeeper instance.
Functionally a wrapper around the orv.VaultKeeper type.

Companion to the leaf implementation in leaf/main.py.
*/
package main

import (
	"fmt"
	"net/netip"
	"network-bois-orv/pkg/orv"
	"os"
	"os/signal"
)

func main() {
	addr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		panic(err)
	}

	var vkid uint64 = 1

	vk, err := orv.NewVaultKeeper(vkid,
		addr,
	)
	if err != nil {
		panic(err)
	}

	if err := vk.Start(); err != nil {
		panic(err)
	}
	fmt.Println("Send a SIGINT to kill the program")

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt)
	<-done

	fmt.Println("SIGINT captured. Cleaning up....")
	vk.Terminate()
}
