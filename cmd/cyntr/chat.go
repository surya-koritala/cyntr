package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func runChat(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: cyntr chat <tenant> <agent>")
		fmt.Println("  Opens an interactive chat session with an agent.")
		fmt.Println("  Commands: /clear, /quit, /help")
		return
	}

	tenant := args[0]
	agentName := args[1]
	apiURL := os.Getenv("CYNTR_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:7700"
	}
	apiKey := os.Getenv("CYNTR_API_KEY")

	fmt.Printf("\n  Cyntr Chat — %s/%s\n", tenant, agentName)
	fmt.Println("  Type a message and press Enter. Commands: /clear, /quit, /help")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for long inputs

	for {
		fmt.Print("You: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle commands
		switch input {
		case "/quit", "/exit", "/q":
			fmt.Println("\n  Goodbye!")
			return
		case "/clear", "/reset":
			// Call clear session
			clearURL := fmt.Sprintf("%s/api/v1/tenants/%s/agents/%s/chat", apiURL, tenant, agentName)
			sendChatMessage(clearURL, "clear", apiKey)
			fmt.Println("  Session cleared.")
			fmt.Println()
			continue
		case "/help":
			fmt.Println("  Commands:")
			fmt.Println("    /clear   — Clear conversation history")
			fmt.Println("    /quit    — Exit chat")
			fmt.Println("    /help    — Show this help")
			fmt.Println()
			continue
		}

		// Send message via streaming SSE
		streamURL := fmt.Sprintf("%s/api/v1/tenants/%s/agents/%s/stream?message=%s",
			apiURL, tenant, agentName, urlEncode(input))
		if apiKey != "" {
			streamURL += "&key=" + urlEncode(apiKey)
		}

		fmt.Print("\nAgent: ")
		err := streamResponse(streamURL)
		if err != nil {
			// Fallback to non-streaming
			chatURL := fmt.Sprintf("%s/api/v1/tenants/%s/agents/%s/chat", apiURL, tenant, agentName)
			resp, chatErr := sendChatMessage(chatURL, input, apiKey)
			if chatErr != nil {
				fmt.Printf("\n  Error: %s\n\n", chatErr)
				continue
			}
			fmt.Printf("%s\n\n", resp)
			continue
		}
		fmt.Println()
		fmt.Println()
	}
}

func streamResponse(url string) error {
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := line[6:]
			var event struct {
				Type    string `json:"type"`
				Content string `json:"content"`
			}
			if json.Unmarshal([]byte(data), &event) == nil {
				switch event.Type {
				case "thinking":
					fmt.Print("...")
				case "text":
					fmt.Print(event.Content)
				}
			}
		}
		if line == "event: done" {
			break
		}
	}
	return nil
}

func sendChatMessage(url, message, apiKey string) (string, error) {
	body := fmt.Sprintf(`{"message":%q}`, message)
	req, err := http.NewRequest("POST", url, strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != nil {
		return "", fmt.Errorf("%s", result.Error.Message)
	}
	return result.Data.Content, nil
}

func urlEncode(s string) string {
	// Simple URL encoding for query params
	s = strings.ReplaceAll(s, " ", "+")
	s = strings.ReplaceAll(s, "&", "%26")
	s = strings.ReplaceAll(s, "=", "%3D")
	s = strings.ReplaceAll(s, "?", "%3F")
	s = strings.ReplaceAll(s, "#", "%23")
	return s
}
