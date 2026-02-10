package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type QueryRequest struct {
	Query string `json:"query"`
}

type QueryResponse struct {
	Answer string `json:"answer"`
	Error  string `json:"error,omitempty"`
}

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "Zenith server address")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Please provide a query (e.g., 'How many errors in the last hour?')")
		os.Exit(1)
	}

	query := strings.Join(args, " ")
	reqBody, err := json.Marshal(QueryRequest{Query: query})
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(fmt.Sprintf("%s/query", *serverAddr), "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Printf("Error contacting server at %s: %v\n", *serverAddr, err)
		fmt.Println("Is the zenith-server running?")
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Error reading response: %v\n", err)
		os.Exit(1)
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Server returned error (Status %d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	var qResp QueryResponse
	if err := json.Unmarshal(body, &qResp); err != nil {
		fmt.Printf("Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if qResp.Error != "" {
		fmt.Printf("Server Error: %s\n", qResp.Error)
		os.Exit(1)
	}

	fmt.Println("\n--- Zenith Analysis ---")
	fmt.Println(qResp.Answer)
}
