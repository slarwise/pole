package main

import (
	"fmt"
	"sync"
)

func main() {
	entry := DirEnt{
		IsDir: true,
		Name:  "mountA",
	}
	vault := VaultClient{
		Addr:  "http://127.0.0.1:8200",
		Token: "dev-only-token",
	}
	recurse(vault, entry)
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
	entries := vault.listDir(entry.Name)
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
}

func (v VaultClient) listDir(name string) []DirEnt {
	if name == "mountA" {
		return []DirEnt{
			{IsDir: true, Name: "dirA"},
			{IsDir: false, Name: "secret2"},
			{IsDir: true, Name: "dirB"},
		}
	} else if name == "dirA" {
		return []DirEnt{
			{IsDir: false, Name: "dirA/secret1"},
		}
	} else if name == "dirB" {
		return []DirEnt{
			{IsDir: false, Name: "dirB/secret3"},
			{IsDir: true, Name: "dirB/dirC"},
		}
	} else if name == "dirB/dirC" {
		return []DirEnt{
			{IsDir: false, Name: "dirB/dirC/secret4"},
			{IsDir: false, Name: "dirB/dirC/secret5"},
		}
	}
	panic(fmt.Sprintf("Unknown dir %s", name))
}

func (v VaultClient) getSecret(name string) string {
	switch name {
	case "dirA/secret1":
		return "password1"
	case "secret2":
		return "password2"
	case "dirB/secret3":
		return "password3"
	case "dirB/dirC/secret4":
		return "password4"
	case "dirB/dirC/secret5":
		return "password5"
	default:
		panic(fmt.Sprintf("Unknown secret %s", name))
	}
}
