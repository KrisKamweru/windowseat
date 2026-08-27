package ptyusage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type authStatus struct {
	LoggedIn         bool   `json:"loggedIn"`
	SubscriptionType string `json:"subscriptionType"`
}

func CheckServerManagedPolicyUnavailable(commandPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	command := exec.CommandContext(ctx, commandPath, "auth", "status", "--json")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return fmt.Errorf("Claude authentication status cannot be read")
	}
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return fmt.Errorf("Claude authentication status cannot start")
	}

	var status authStatus
	decodeErr := json.NewDecoder(io.LimitReader(stdout, 64*1024)).Decode(&status)
	waitErr := command.Wait()
	if decodeErr != nil || waitErr != nil {
		return fmt.Errorf("Claude authentication status is malformed")
	}
	if !status.LoggedIn {
		return fmt.Errorf("Claude is not authenticated")
	}
	if !personalSubscription(status.SubscriptionType) {
		return fmt.Errorf("server-managed Claude settings cannot be ruled out for this subscription")
	}
	return nil
}

func personalSubscription(subscriptionType string) bool {
	switch strings.ToLower(strings.TrimSpace(subscriptionType)) {
	case "free", "pro", "max":
		return true
	default:
		return false
	}
}
