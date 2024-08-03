package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

func main() {
	entrypoint := DirEnt{
		IsDir: true,
		Name:  "/",
	}
	vault := VaultClient{
		Addr:  "http://127.0.0.1:8200",
		Token: "dev-only-token",
		Mount: "secret",
	}
	recurse(vault, entrypoint)
}

type DirEnt struct {
	IsDir bool
	Name  string
}

func recurse(vault VaultClient, entry DirEnt) {
	if !entry.IsDir {
		secret := vault.getSecret(entry.Name)
		fmt.Printf("%s - %s\n", entry.Name, secret)
		return
	}
	subs := vault.listDir(entry.Name)
	entries := []DirEnt{}
	for _, sub := range subs {
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
			recurse(vault, e)
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
	url := fmt.Sprintf("%s/v1/%s/data/%s", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(err)
	}
	request.Header.Set("X-Vault-Token", v.Token)
	request.Header.Set("X-Vault-Request", "true")
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
