package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

func envOrPanic(name string) string {
	value, found := os.LookupEnv(name)
	if !found {
		panic(fmt.Sprintf("Enivronment variable %s must be set", name))
	}
	return value
}

func main() {
	vault := VaultClient{
		Addr:  envOrPanic("VAULT_ADDR"),
		Token: envOrPanic("VAULT_TOKEN"),
		Mount: envOrPanic("VAULT_MOUNT"),
	}
	entrypoint := DirEnt{
		IsDir: true,
		Name:  "/",
	}
	keys := getKeys(vault, entrypoint)
	fmt.Println(keys)
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
	relativeEntries := vault.listDir(entry.Name)
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

func (v VaultClient) listDir(name string) []DirEnt {
	url := fmt.Sprintf("%s/v1/%s/metadata%s?list=true", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("X-Vault-Token", v.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	if response.StatusCode != 200 {
		panic(fmt.Sprintf("Error listing entries on url %s: %s", url, response.Status))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	listResponse := struct {
		Data struct {
			Keys []string
		}
	}{}
	if err := json.Unmarshal(body, &listResponse); err != nil {
		panic(err)
	}
	entries := []DirEnt{}
	for _, key := range listResponse.Data.Keys {
		e := DirEnt{Name: key}
		if strings.HasSuffix(key, "/") {
			e.IsDir = true
		}
		entries = append(entries, e)
	}
	return entries
}

type Secret map[string]string

func (v VaultClient) getSecret(name string) Secret {
	url := fmt.Sprintf("%s/v1/%s/data%s", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("X-Vault-Token", v.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(err)
	}
	if response.StatusCode != 200 {
		panic(fmt.Sprintf("Error getting secret on url %s: %s", url, response.Status))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		panic(err)
	}
	getResponse := struct {
		Data struct {
			Data map[string]string
		}
	}{}
	if err := json.Unmarshal(body, &getResponse); err != nil {
		panic(err)
	}
	return getResponse.Data.Data
}
