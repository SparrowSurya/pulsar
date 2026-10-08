package main

import (
	"pulsar/internal/config"
	_ "pulsar/internal/plugins/chat"
	_ "pulsar/internal/plugins/bingo"
	_ "pulsar/internal/plugins/tictactoe"
	_ "pulsar/internal/plugins/uno"
	"pulsar/internal/server"
)

func main() {
	cfg := config.LoadEnv()

	s := server.Server{}
	s.Setup()
	s.Run(cfg.Address())
}
