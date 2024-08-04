package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
)

func mustGetEnv(name string) string {
	value, found := os.LookupEnv(name)
	if !found {
		fatal(fmt.Sprintf("Environment variable %s must be set", name))
	}
	return value
}

func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func main() {
	log.SetFlags(0) // Disable the timestamp
	vault := VaultClient{
		Addr:  mustGetEnv("VAULT_ADDR"),
		Token: mustGetEnv("VAULT_TOKEN"),
		Mount: mustGetEnv("VAULT_MOUNT"),
	}
	if len(os.Args) < 2 {
		fatal("Must provide subcommand `tree` or `get`")
	}
	subcommand := os.Args[1]
	if subcommand == "tree" {
		entrypoint := "/"
		if len(os.Args) > 2 {
			entrypoint = os.Args[2]
			if !strings.HasPrefix(entrypoint, "/") {
				entrypoint = "/" + entrypoint
			}
			if !strings.HasSuffix(entrypoint, "/") {
				entrypoint += "/"
			}
		}
		keys := getKeys(vault, DirEnt{IsDir: true, Name: entrypoint})
		for _, key := range keys {
			fmt.Println(key)
		}
	} else if subcommand == "get" {
		if len(os.Args) < 3 {
			fatal("Must provide the name of the secret, e.g. ./pole3 get my-secret")
		}
		key := os.Args[2]
		if !strings.HasPrefix(key, "/") {
			key = "/" + key
		}
		secret, err := vault.getSecret(key)
		if err != nil {
			fatal("Failed to get secret", "key", key, "err", err)
		}
		output, err := json.MarshalIndent(secret, "", "  ")
		if err != nil {
			fatal("Could not marshal secret as json", "error", err)
		}
		fmt.Println(string(output))
	} else {
		fatal(fmt.Sprintf("Subcommand must be one of `tree` or `get`, got %s", subcommand))
	}
}

func getKeys(vault VaultClient, entrypoint DirEnt) []string {
	recv := make(chan string)
	go func() {
		recurse(recv, vault, entrypoint)
		close(recv)
	}()
	keys := []string{}
	for key := range recv {
		keys = append(keys, key)
	}
	return keys
}

type DirEnt struct {
	IsDir bool
	Name  string
}

func recurse(recv chan string, vault VaultClient, entry DirEnt) {
	if !entry.IsDir {
		recv <- entry.Name
		return
	}
	relativeEntries, err := vault.listDir(entry.Name)
	if err != nil {
		slog.Error("Failed to list directory", "directory", entry.Name, "err", err.Error())
		return
	}
	entries := []DirEnt{}
	for _, sub := range relativeEntries {
		entries = append(entries, DirEnt{
			IsDir: sub.IsDir,
			Name:  entry.Name + sub.Name,
		})
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(entry DirEnt) {
			defer wg.Done()
			recurse(recv, vault, e)
		}(e)
	}
	wg.Wait()
}

type VaultClient struct {
	Addr  string
	Token string
	Mount string
}

func (v VaultClient) listDir(name string) ([]DirEnt, error) {
	url := fmt.Sprintf("%s/v1/%s/metadata%s?list=true", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []DirEnt{}, fmt.Errorf("Failed to create request: %s", err)
	}
	request.Header.Set("X-Vault-Token", v.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return []DirEnt{}, fmt.Errorf("Failed to perform request: %s", err)
	}
	if response.StatusCode == 403 {
		slog.Info("Forbidden to list dir", "dir", name, "url", url)
		return []DirEnt{}, nil
	} else if response.StatusCode != 200 {
		return []DirEnt{}, fmt.Errorf("Got %s on url %s", response.Status, url)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return []DirEnt{}, fmt.Errorf("Failed to read response body: %s", err)
	}
	listResponse := struct {
		Data struct {
			Keys []string
		}
	}{}
	if err := json.Unmarshal(body, &listResponse); err != nil {
		return []DirEnt{}, fmt.Errorf("Failed to parse response body %s: %s", string(body), err)
	}
	entries := []DirEnt{}
	for _, key := range listResponse.Data.Keys {
		e := DirEnt{Name: key}
		if strings.HasSuffix(key, "/") {
			e.IsDir = true
		}
		entries = append(entries, e)
	}
	return entries, nil
}

type Secret struct {
	Data struct {
		Data     map[string]interface{} `json:"data"`
		Metadata map[string]interface{} `json:"metadata"`
	} `json:"data"`
}

func (v VaultClient) getSecret(name string) (Secret, error) {
	url := fmt.Sprintf("%s/v1/%s/data%s", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return Secret{}, fmt.Errorf("Failed to create request: %s", err)
	}
	request.Header.Set("X-Vault-Token", v.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return Secret{}, fmt.Errorf("Failed to perform request: %s", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	var secret Secret
	if err := json.Unmarshal(body, &secret); err != nil {
		return Secret{}, fmt.Errorf("Failed to unmarshal response body %s: %s", string(body), err.Error())
	}
	// 404 can mean that the secret has been deleted, but it will still
	// be listed. Supposedly all status codes above 400 return an
	// error body. This is not true in this case. I guess we can look
	// at the body and see if it has errors, if not the response is
	// still valid and we can show the data.
	// https://developer.hashicorp.com/vault/api-docs#error-response
	isErrorForRealForReal := secret.Data.Data == nil && secret.Data.Metadata == nil
	if response.StatusCode != 200 && isErrorForRealForReal {
		return Secret{}, fmt.Errorf("Got %s on url %s", response.Status, url)
	}
	return secret, nil
}
