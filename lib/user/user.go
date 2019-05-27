package goterrauser

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// User represents a user
type User struct {
	Logged     bool
	ID         string
	Admin      bool
	Email      string
	Namespaces map[string]bool // map of namespace names, if true user is owner of namespace else only a member
}

// APIData is message for auth service url /api/auth
type APIData struct {
	Key string `json:"key"`
}

// Check checks X-API-Key authorization content and returns user info
func Check(apiKey string) (user User, err error) {
	err = nil
	user = User{}

	url := os.Getenv("GOT_PROXY")
	if os.Getenv("GOT_PROXY_AUTH") != "" {
		url = os.Getenv("GOT_PROXY_AUTH")
	}

	client := &http.Client{}
	remote := []string{url, "auth", "api"}
	data := APIData{Key: apiKey}
	jsonData := new(bytes.Buffer)
	json.NewEncoder(jsonData).Encode(data)
	req, _ := http.NewRequest("POST", strings.Join(remote, "/"), jsonData)
	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return user, errors.New("failed to contact auth service")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return user, fmt.Errorf("auth error %d", resp.StatusCode)
	}
	respData := &User{}
	json.NewDecoder(resp.Body).Decode(respData)
	return *respData, err
}
