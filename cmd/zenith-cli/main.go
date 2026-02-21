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
	InteractionID int64  `json:"interaction_id,omitempty"`
	Answer        string `json:"answer"`
	Error         string `json:"error,omitempty"`
}

func main() {
	serverAddr := flag.String("server", "http://localhost:8080", "Zenith server address")
	feedbackPtr := flag.String("feedback", "", "Provide feedback on a previous interaction ('good' or 'bad')")
	idPtr := flag.Int64("id", 0, "The Interaction ID to provide feedback for")
	flag.Parse()

	if *feedbackPtr != "" {
		if *idPtr == 0 {
			fmt.Println("Error: --id is required when providing --feedback")
			os.Exit(1)
		}

		val := 0
		if strings.ToLower(*feedbackPtr) == "good" {
			val = 1
		} else if strings.ToLower(*feedbackPtr) == "bad" {
			val = -1
		} else {
			fmt.Println("Error: --feedback must be 'good' or 'bad'")
			os.Exit(1)
		}

		sendFeedback(*serverAddr, *idPtr, val)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Please provide a query (e.g., 'How many errors in the last hour?'), 'recommend', or use --feedback")
		os.Exit(1)
	}

	if args[0] == "recommend" {
		resp, err := http.Get(fmt.Sprintf("%s/recommend", *serverAddr))
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

		fmt.Println("\n--- Zenith Recommendations ---")
		fmt.Println(qResp.Answer)
		if qResp.InteractionID != 0 {
			fmt.Printf("\n[Interaction ID: %d] To provide feedback, use: zenith-cli --id %d --feedback good|bad\n", qResp.InteractionID, qResp.InteractionID)
		}
		return
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
	if qResp.InteractionID != 0 {
		fmt.Printf("\n[Interaction ID: %d] To provide feedback, use: zenith-cli --id %d --feedback good|bad\n", qResp.InteractionID, qResp.InteractionID)
	}
}

func sendFeedback(serverAddr string, id int64, feedback int) {
	reqBody := fmt.Sprintf(`{"interaction_id": %d, "feedback": %d}`, id, feedback)

	resp, err := http.Post(fmt.Sprintf("%s/feedback", serverAddr), "application/json", bytes.NewBuffer([]byte(reqBody)))
	if err != nil {
		fmt.Printf("Error sending feedback: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Server returned error (Status %d): %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}

	fmt.Printf("Feedback recorded for Interaction ID: %d\n", id)
}
