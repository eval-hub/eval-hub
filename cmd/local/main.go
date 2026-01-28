package main

import (
	"log"

	"github.com/eval-hub/eval-hub/internal/config"
	"github.com/eval-hub/eval-hub/internal/logging"
)

func main() {
	logger, _, err := logging.NewLogger()
	if err != nil {
		log.Fatal(err)
	}

	providerConfigs, err := config.LoadProviderConfigs(logger)
	if err != nil {
		log.Fatal(err)
	}

	println(providerConfigs["rags"])
}
