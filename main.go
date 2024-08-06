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

	"github.com/gdamore/tcell/v2"
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
	switch subcommand {
	case "tree":
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
	case "get":
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
	case "interactive":
		slog.SetLogLoggerLevel(slog.LevelError)
		screen, err := tcell.NewScreen()
		if err != nil {
			fatal("Failed to create a terminal screen: %s", err.Error())
		}
		if err := screen.Init(); err != nil {
			fatal("Failed to initialize terminal screen: %s", err)
		}
		screen.EnablePaste()
		screen.Clear()
		result := []byte{}
		filteredKeys := []string{}
		quit := func() {
			// You have to catch panics in a defer, clean up, and
			// re-raise them - otherwise your application can
			// die without leaving any diagnostic trace.
			maybePanic := recover()
			screen.Fini()
			if maybePanic != nil {
				panic(maybePanic)
			}
			if len(result) != 0 {
				fmt.Printf("%s\n", result)
			}
		}
		defer quit()
		_, height := screen.Size()
		prompt := ""
		selectedIndex := 0
		drawPrompt(screen, height, prompt)
		drawLoadingScreen(screen, height)
		screen.Show()
		keys := getKeys(vault, DirEnt{IsDir: true, Name: "/"})
		for {
			ev := screen.PollEvent()
			switch ev := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyEscape, tcell.KeyCtrlC:
					return
				case tcell.KeyEnter:
					secret, err := vault.getSecret(filteredKeys[selectedIndex])
					if err != nil {
						panic("oopa")
					}
					bytes, err := json.MarshalIndent(secret, "", "  ")
					if err != nil {
						panic("gangnam")
					}
					result = bytes
					return
				case tcell.KeyBackspace, tcell.KeyBackspace2:
					if len(prompt) > 0 {
						prompt = prompt[:len(prompt)-1]
					}
				case tcell.KeyCtrlU:
					prompt = ""
				case tcell.KeyCtrlK, tcell.KeyCtrlP:
					selectedIndex = min(len(filteredKeys)-1, selectedIndex+1)
				case tcell.KeyCtrlJ, tcell.KeyCtrlN:
					selectedIndex = max(0, selectedIndex-1)
				case tcell.KeyRune:
					prompt += string(ev.Rune())
					selectedIndex = 0
				}
			}
			width, height := screen.Size()
			filteredKeys = []string{}
			for _, k := range keys {
				if strings.Contains(k, prompt) {
					filteredKeys = append(filteredKeys, k)
				}
			}
			screen.Clear()
			drawKeys(screen, width, height, filteredKeys, selectedIndex)
			drawStats(screen, height, filteredKeys)
			drawPrompt(screen, height, prompt)
			if len(filteredKeys) > 0 {
				secret, err := vault.getSecret(filteredKeys[selectedIndex])
				if err != nil {
					panic("style")
				}
				drawSecret(screen, width, height, secret)
			}
			screen.Show()
		}
	default:
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

func drawLine(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for _, r := range []rune(text) {
		s.SetContent(x, y, r, nil, style)
		x++
	}
}

// TODO: Handle going out of bounds
func drawKeys(s tcell.Screen, width, height int, keys []string, selectedIndex int) {
	keys = keys[:min(height-2, len(keys))]
	y := height - 3
	for _, line := range keys {
		line = line[:min(width/2, len(line))]
		if y == (height - 3 - selectedIndex) {
			drawLine(s, 0, y, tcell.StyleDefault.Background(tcell.ColorRed), " ")
			drawLine(s, 1, y, tcell.StyleDefault.Background(tcell.ColorBlack), " ")
			drawLine(s, 2, y, tcell.StyleDefault.Background(tcell.ColorBlack), line)
		} else {
			drawLine(s, 2, y, tcell.StyleDefault, line)
		}
		y--
	}
}

// TODO: Add color so it looks like jq-ish
func drawSecret(s tcell.Screen, width, height int, secret Secret) {
	x := width / 2
	y := 0
	s.SetContent(x, y, rune("{"[0]), nil, tcell.StyleDefault)
	y++
	drawLine(s, x+2, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), `"data": {`)
	y++
	i := 0
	for k, v := range secret.Data.Data {
		kStr := fmt.Sprintf(`"%s": `, k)
		drawLine(s, x+4, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), kStr)
		vStart := x + 4 + len(kStr)
		switch v.(type) {
		case string:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, v))
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, v))
			}
		case []interface{}:
			if len(v.([]interface{})) == 0 {
				if i < len(secret.Data.Data)-1 {
					drawLine(s, vStart, y, tcell.StyleDefault, "[],")
				} else {
					drawLine(s, vStart, y, tcell.StyleDefault, "[]")
				}
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, "[")
				y++
				for i, element := range v.([]interface{}) {
					if i < len(v.([]interface{}))-1 {
						drawLine(s, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, element.(string)))
					} else {
						drawLine(s, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, element.(string)))
					}
					y++
				}
				drawLine(s, x+4, y, tcell.StyleDefault, "],")
			}
		case nil:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault, "null,")
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, "null")
			}
		default:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v,", v))
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v", v))
			}
		}
		y++
		i++
	}
	s.SetContent(x+2, y, rune("}"[0]), nil, tcell.StyleDefault)
	y++

	drawLine(s, x+2, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), `"metadata": {`)
	y++
	i = 0
	for k, v := range secret.Data.Metadata {
		kStr := fmt.Sprintf(`"%s": `, k)
		drawLine(s, x+4, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), kStr)
		vStart := x + 4 + len(kStr)
		switch v.(type) {
		case string:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, v))
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, v))
			}
		case []interface{}:
			if len(v.([]interface{})) == 0 {
				if i < len(secret.Data.Data)-1 {
					drawLine(s, vStart, y, tcell.StyleDefault, "[],")
				} else {
					drawLine(s, vStart, y, tcell.StyleDefault, "[]")
				}
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, "[")
				y++
				for i, element := range v.([]interface{}) {
					if i < len(v.([]interface{}))-1 {
						drawLine(s, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, element.(string)))
					} else {
						drawLine(s, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, element.(string)))
					}
					y++
				}
				drawLine(s, x+4, y, tcell.StyleDefault, "],")
			}
		case nil:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault, "null,")
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, "null")
			}
		default:
			if i < len(secret.Data.Data)-1 {
				drawLine(s, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v,", v))
			} else {
				drawLine(s, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v", v))
			}
		}
		y++
		i++
	}
	s.SetContent(x+2, y, rune("}"[0]), nil, tcell.StyleDefault)
	y++
	s.SetContent(x, y, rune("}"[0]), nil, tcell.StyleDefault)
}

func drawStats(s tcell.Screen, height int, keys []string) {
	nKeys := len(keys)
	drawLine(s, 2, height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), fmt.Sprintf("%d", nKeys))
}

func drawPrompt(s tcell.Screen, height int, prompt string) {
	drawLine(s, 0, height-1, tcell.StyleDefault.Bold(true), ">")
	drawLine(s, 2, height-1, tcell.StyleDefault, prompt)
}

func drawLoadingScreen(s tcell.Screen, height int) {
	drawLine(s, 2, height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), "Loading...")
}
