package main

import (
	"net/netip"
	"network-bois-orv/pkg/orv"
	"os"

	"github.com/rs/zerolog"
)

func main() {
	addr, err := netip.ParseAddrPort("[::1]:8080")
	if err != nil {
		panic(err)
	}

	vk := orv.NewVaultKeeper(1,
		zerolog.New(zerolog.ConsoleWriter{
			Out:         os.Stdout,
			FieldsOrder: []string{"p", "tag"},
			TimeFormat:  "15:04:05",
		}).With().
			Int("p", 1).
			Timestamp().
			Caller().
			Logger().Level(zerolog.DebugLevel),
		addr,
	)

	vk.Start()
}
