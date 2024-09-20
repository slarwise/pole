package vault

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
)

type Client struct {
	Addr  string
	Token string
}

type dirEnt struct {
	IsDir bool
	Name  string
}

var cachedKeys = make(map[string][]string)

func (c Client) GetKeys(mount string) []string {
	if keys, found := cachedKeys[mount]; found {
		return keys
	}
	entrypoint := dirEnt{
		IsDir: true,
		Name:  "/",
	}
	recv := make(chan string)
	go func() {
		c.recurse(recv, mount, entrypoint)
		close(recv)
	}()
	keys := []string{}
	for key := range recv {
		keys = append(keys, key)
	}
	cachedKeys[mount] = keys
	return keys
}

func (c Client) recurse(recv chan string, mount string, entry dirEnt) {
	if !entry.IsDir {
		recv <- entry.Name
		return
	}
	relativeEntries, err := c.listDir(mount, entry.Name)
	if err != nil {
		slog.Error("Failed to list directory", "directory", entry.Name, "err", err.Error())
		return
	}
	entries := []dirEnt{}
	for _, sub := range relativeEntries {
		entries = append(entries, dirEnt{
			IsDir: sub.IsDir,
			Name:  entry.Name + sub.Name,
		})
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(entry dirEnt) {
			defer wg.Done()
			c.recurse(recv, mount, e)
		}(e)
	}
	wg.Wait()
}

func (c Client) listDir(mount string, name string) ([]dirEnt, error) {
	url := fmt.Sprintf("%s/v1/%s/metadata%s?list=true", c.Addr, mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return []dirEnt{}, fmt.Errorf("Failed to create request: %s", err)
	}
	request.Header.Set("X-Vault-Token", c.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return []dirEnt{}, fmt.Errorf("Failed to perform request: %s", err)
	}
	if response.StatusCode == 403 {
		slog.Info("Forbidden to list dir", "dir", name, "url", url)
		return []dirEnt{}, nil
	} else if response.StatusCode != 200 {
		return []dirEnt{}, fmt.Errorf("Got %s on url %s", response.Status, url)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return []dirEnt{}, fmt.Errorf("Failed to read response body: %s", err)
	}
	listResponse := struct {
		Data struct {
			Keys []string
		}
	}{}
	if err := json.Unmarshal(body, &listResponse); err != nil {
		return []dirEnt{}, fmt.Errorf("Failed to parse response body %s: %s", string(body), err)
	}
	entries := []dirEnt{}
	for _, key := range listResponse.Data.Keys {
		e := dirEnt{Name: key}
		if strings.HasSuffix(key, "/") {
			e.IsDir = true
		}
		entries = append(entries, e)
	}
	return entries, nil
}

type Secret struct {
	Url  string `json:"url"`
	Data struct {
		Data     map[string]interface{} `json:"data"`
		Metadata map[string]interface{} `json:"metadata"`
	} `json:"data"`
}

var cachedSecrets = make(map[string]Secret)

func (c Client) GetSecret(mount, name string) Secret {
	if secret, found := cachedSecrets[name]; found {
		return secret
	}
	url := fmt.Sprintf("%s/v1/%s/data%s", c.Addr, mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Errorf("Failed to create request: %s", err))
	}
	request.Header.Set("X-Vault-Token", c.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(fmt.Errorf("Failed to perform request: %s", err))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	var secret Secret
	if err := json.Unmarshal(body, &secret); err != nil {
		panic(fmt.Errorf("Failed to unmarshal response body %s: %s", string(body), err.Error()))
	}
	// 404 can mean that the secret has been deleted, but it will still
	// be listed. Supposedly all status codes above 400 return an
	// error body. This is not true in this case. I guess we can look
	// at the body and see if it has errors, if not the response is
	// still valid and we can show the data.
	// https://developer.hashicorp.com/vault/api-docs#error-response
	isErrorForRealForReal := secret.Data.Data == nil && secret.Data.Metadata == nil
	if response.StatusCode != 200 && isErrorForRealForReal {
		panic(fmt.Errorf("Got %s on url %s", response.Status, url))
	}
	secret.Url = fmt.Sprintf("%s/ui/vault/secrets/%s/show%s", c.Addr, mount, name)
	cachedSecrets[name] = secret
	return secret
}

type MountResponse struct {
	Data struct {
		Secret map[string]Mount
	}
}

type Mount struct {
	Type string
}

func (c Client) GetMounts() []string {
	url := fmt.Sprintf("%s/v1/sys/internal/ui/mounts", c.Addr)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Errorf("Failed to create request: %s", err))
	}
	request.Header.Set("X-Vault-Token", c.Token)
	request.Header.Set("Accept", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		panic(fmt.Errorf("Failed to perform request: %s", err))
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	var mounts MountResponse
	if err := json.Unmarshal(body, &mounts); err != nil {
		panic(fmt.Errorf("failed to unmarshal response body %s: %s", string(body), err))
	}
	mountNames := []string{}
	for k, v := range mounts.Data.Secret {
		if v.Type == "kv" {
			mountNames = append(mountNames, strings.TrimSuffix(k, "/"))
		}
	}
	slices.Sort(mountNames)
	return mountNames
}
