package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ninja0404/pump-go-sdk/pkg/vanity"
)

func main() {
	ctx := context.Background()

	// Test different suffix lengths
	suffixes := []string{"a", "ab", "abc", "pump"}

	for _, suffix := range suffixes {
		fmt.Printf("🔍 Searching for address ending with '%s'...\n", suffix)
		fmt.Printf("   Estimated difficulty: ~%d attempts\n", vanity.EstimateDifficulty(0, len(suffix)))

		startTime := time.Now()

		result, err := vanity.Generate(ctx, vanity.Options{
			Suffix:  suffix,
			Timeout: 5 * time.Minute,
		})
		if err != nil {
			log.Printf("   ❌ Failed: %v\n\n", err)
			continue
		}

		fmt.Printf("   ✅ Found in %s (%d attempts)\n", result.Duration, result.Attempts)
		fmt.Printf("   Address: %s\n", result.PublicKey)
		fmt.Printf("   Rate: %.0f attempts/sec\n\n", float64(result.Attempts)/result.Duration.Seconds())

		// Only test short suffixes to save time
		if time.Since(startTime) > 30*time.Second {
			fmt.Println("⏱️  Skipping longer patterns to save time...")
			break
		}
	}
}
