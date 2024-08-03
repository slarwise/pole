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
	recurse(entry)
}

type DirEnt struct {
	IsDir bool
	Name  string
}

func recurse(entry DirEnt) {
	if !entry.IsDir {
		fmt.Printf("Found secret: %s\n", entry.Name)
		return
	}
	entries := listDir(entry.Name)
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(entry DirEnt) {
			defer wg.Done()
			recurse(e)
		}(e)
	}
	wg.Wait()
}

func listDir(name string) []DirEnt {
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

func getSecret(name string) string {
	if name == "dirA/secret1" {
		return "password1"
	} else if name == "secret2" {
		return "password2"
	}
	panic(fmt.Sprintf("Unknown secret %s", name))
}
