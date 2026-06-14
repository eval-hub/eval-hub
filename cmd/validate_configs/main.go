package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/eval-hub/eval-hub/internal/eval_hub/config"
	"github.com/eval-hub/eval-hub/internal/eval_hub/validation"
	"github.com/eval-hub/eval-hub/internal/logging"
)

func main() {
	configDir := flag.String("config-dir", "config", "Directory containing providers/ and collections/ subdirectories")
	flag.Parse()

	logger := logging.FallbackLogger()
	validate := validation.NewValidator()

	providers, err := config.LoadProviderConfigs(logger, validate, *configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "provider configs: %v\n", err)
		os.Exit(1)
	}
	if len(providers) == 0 {
		fmt.Fprintln(os.Stderr, "provider configs: no providers loaded")
		os.Exit(1)
	}

	collections, err := config.LoadCollectionConfigs(logger, validate, *configDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "collection configs: %v\n", err)
		os.Exit(1)
	}
	if len(collections) == 0 {
		fmt.Fprintln(os.Stderr, "collection configs: no collections loaded")
		os.Exit(1)
	}

	providerIDs := sortedKeys(providers)
	collectionIDs := sortedKeys(collections)

	fmt.Printf("validated %d providers: %v\n", len(providers), providerIDs)
	fmt.Printf("validated %d collections: %v\n", len(collections), collectionIDs)
}

func sortedKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j])
	})
	return keys
}
