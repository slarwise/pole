package vault

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

type Client struct {
	Addr  string
	Token string
	Mount string
}

type DirEnt struct {
	IsDir bool
	Name  string
}

func (v Client) ListDir(name string) ([]DirEnt, error) {
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

var cachedSecrets = make(map[string]Secret)

func (v Client) GetSecret(name string) Secret {
	if secret, found := cachedSecrets[name]; found {
		return secret
	}
	url := fmt.Sprintf("%s/v1/%s/data%s", v.Addr, v.Mount, name)
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		panic(fmt.Errorf("Failed to create request: %s", err))
	}
	request.Header.Set("X-Vault-Token", v.Token)
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
	cachedSecrets[name] = secret
	return secret
}
