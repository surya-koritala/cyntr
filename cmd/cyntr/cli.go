package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func apiURL() string {
	if u := os.Getenv("CYNTR_API_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:7700"
}

func apiGet(path string) {
	resp, err := http.Get(apiURL() + path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	prettyPrint(resp.Body)
}

func apiPost(path string, payload any) {
	body, _ := json.Marshal(payload)
	resp, err := http.Post(apiURL()+path, "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	prettyPrint(resp.Body)
}

func prettyPrint(r io.Reader) {
	var raw json.RawMessage
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		fmt.Fprintf(os.Stderr, "error reading response: %v\n", err)
		return
	}
	var pretty bytes.Buffer
	json.Indent(&pretty, raw, "", "  ")
	fmt.Println(pretty.String())
}

func runCLI(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: cyntr <command> [subcommand] [args]")
		os.Exit(1)
	}

	switch args[0] {
	case "tenant":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cyntr tenant <list>")
			os.Exit(1)
		}
		switch args[1] {
		case "list":
			apiGet("/api/v1/tenants")
		default:
			fmt.Fprintf(os.Stderr, "unknown tenant command: %s\n", args[1])
			os.Exit(1)
		}

	case "agent":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cyntr agent <create|list|chat> ...")
			os.Exit(1)
		}
		switch args[1] {
		case "create":
			if len(args) < 4 {
				fmt.Fprintln(os.Stderr, "usage: cyntr agent create <tenant> <name> [--model <model>]")
				os.Exit(1)
			}
			tenant, name := args[2], args[3]
			model := "mock"
			for i, a := range args {
				if a == "--model" && i+1 < len(args) {
					model = args[i+1]
				}
			}
			apiPost(fmt.Sprintf("/api/v1/tenants/%s/agents", tenant), map[string]any{
				"name": name, "model": model, "max_turns": 10,
			})
		case "list":
			if len(args) < 3 {
				fmt.Fprintln(os.Stderr, "usage: cyntr agent list <tenant>")
				os.Exit(1)
			}
			tenant := args[2]
			apiGet(fmt.Sprintf("/api/v1/tenants/%s/agents", tenant))
		case "chat":
			if len(args) < 5 {
				fmt.Fprintln(os.Stderr, "usage: cyntr agent chat <tenant> <agent> <message>")
				os.Exit(1)
			}
			tenant, agentName, message := args[2], args[3], strings.Join(args[4:], " ")
			apiPost(fmt.Sprintf("/api/v1/tenants/%s/agents/%s/chat", tenant, agentName), map[string]string{
				"message": message,
			})
		default:
			fmt.Fprintf(os.Stderr, "unknown agent command: %s\n", args[1])
			os.Exit(1)
		}

	case "audit":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cyntr audit <query>")
			os.Exit(1)
		}
		switch args[1] {
		case "query":
			tenant := ""
			for i, a := range args {
				if a == "--tenant" && i+1 < len(args) {
					tenant = args[i+1]
				}
			}
			path := "/api/v1/audit"
			if tenant != "" {
				path += "?tenant=" + tenant
			}
			apiGet(path)
		default:
			fmt.Fprintf(os.Stderr, "unknown audit command: %s\n", args[1])
			os.Exit(1)
		}

	case "policy":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cyntr policy <test>")
			os.Exit(1)
		}
		switch args[1] {
		case "test":
			tenant, action, tool := "", "", ""
			for i, a := range args {
				if a == "--tenant" && i+1 < len(args) {
					tenant = args[i+1]
				}
				if a == "--action" && i+1 < len(args) {
					action = args[i+1]
				}
				if a == "--tool" && i+1 < len(args) {
					tool = args[i+1]
				}
			}
			apiPost("/api/v1/policies/test", map[string]string{
				"Tenant": tenant, "Action": action, "Tool": tool,
			})
		default:
			fmt.Fprintf(os.Stderr, "unknown policy command: %s\n", args[1])
			os.Exit(1)
		}

	case "federation":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: cyntr federation <peers>")
			os.Exit(1)
		}
		switch args[1] {
		case "peers":
			apiGet("/api/v1/federation/peers")
		default:
			fmt.Fprintf(os.Stderr, "unknown federation command: %s\n", args[1])
			os.Exit(1)
		}

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}
