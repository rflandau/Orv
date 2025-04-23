/*
Leaf instance implementation.

A leaf is any service that able to speak Orv semantics, but does not offer a listener service like VaultKeepers must do.

Companion to the vk implementation in vk/main.go.
*/

package main

func main() {
	// spawn the services we want to offer
	// TODO spin up a dummy server that just serves arbitrary files or just answers /ping

	// send a HELLO to a VK
	// TODO

	// send a JOIN request to that same VK
	// TODO

	// send a REGISTER request to that same VK to register our dummy services
	// TODO (this should probably be one REGISTER per service)

	// set up a heartbeat service to ping our vk every so often, per service (but less than a service's staleness)

}
