package main

import "fmt"

func showPostStartBanner(dashboardURL, apiURL string) {
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────┐")
	fmt.Println("  │  Cyntr is running!                                      │")
	fmt.Println("  │                                                         │")
	fmt.Printf("  │  Dashboard:  %-42s │\n", dashboardURL)
	fmt.Printf("  │  API:        %-42s │\n", apiURL)
	fmt.Println("  │                                                         │")
	fmt.Println("  │  Quick start:                                           │")
	fmt.Println("  │    1. Open the dashboard in your browser                │")
	fmt.Println("  │    2. Create an agent in the Agents tab                 │")
	fmt.Println("  │    3. Start chatting!                                   │")
	fmt.Println("  │                                                         │")
	fmt.Println("  │  Or use the CLI:                                        │")
	fmt.Println("  │    cyntr agent create <tenant> <name> --model claude    │")
	fmt.Println("  │    cyntr agent chat <tenant> <name> \"Hello!\"            │")
	fmt.Println("  │                                                         │")
	fmt.Println("  │  Press Ctrl+C to stop                                   │")
	fmt.Println("  └─────────────────────────────────────────────────────────┘")
	fmt.Println()
}
