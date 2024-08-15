package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/slarwise/pole3/internal/vault"

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

type Ui struct {
	Screen       tcell.Screen
	Keys         []string
	FilteredKeys []string
	Secret       vault.Secret
	Prompt       string
	ViewStart    int
	ViewEnd      int
	Cursor       int
	Width        int
	Height       int
	Result       []byte
	Vault        vault.Client
}

const SCROLL_OFF = 4

func main() {
	log.SetFlags(0) // Disable the timestamp
	vaultClient := vault.Client{
		Addr:  mustGetEnv("VAULT_ADDR"),
		Token: mustGetEnv("VAULT_TOKEN"),
		Mount: mustGetEnv("VAULT_MOUNT"),
	}
	if len(os.Getenv("DEBUG")) > 0 {
		logFile, err := os.Create("./log")
		if err != nil {
			fatal("Failed to create log file", "err", err)
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	} else {
		log.SetOutput(io.Discard)
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		fatal("Failed to create a terminal screen", "err", err)
	}
	if err := screen.Init(); err != nil {
		fatal("Failed to initialize terminal screen", "err", err)
	}
	screen.EnablePaste()
	screen.Clear()
	state := Ui{
		Screen: screen,
		Vault:  vaultClient,
	}
	quit := func() {
		// You have to catch panics in a defer, clean up, and
		// re-raise them - otherwise your application can
		// die without leaving any diagnostic trace.
		errorMsg := recover()
		screen.Fini()
		if errorMsg != nil {
			fmt.Fprintf(os.Stderr, "%s\n", errorMsg)
		} else if len(state.Result) != 0 {
			fmt.Printf("%s\n", state.Result)
		}
	}
	defer quit()
	state.Width, state.Height = screen.Size()
	drawPrompt(state)
	drawLoadingScreen(state)
	screen.Show()
	state.Keys = getKeys(vaultClient, vault.DirEnt{IsDir: true, Name: "/"})
	newKeysView(&state)
	for {
		ev := screen.PollEvent()
		switch ev := ev.(type) {
		case *tcell.EventResize:
			screen.Sync()
			state.Width, state.Height = screen.Size()
			state.ViewEnd = min(nKeysToShow(state.Height), len(state.FilteredKeys))
			if state.ViewStart+state.Cursor >= state.ViewEnd {
				state.Cursor = 0
				state.ViewStart = 0
			}
		case *tcell.EventKey:
			// TODO: Add ability to switch between key-value vault mounts
			switch ev.Key() {
			case tcell.KeyEscape, tcell.KeyCtrlC:
				return
			case tcell.KeyEnter:
				if !(reflect.ValueOf(state.Secret).IsZero()) {
					bytes, err := json.MarshalIndent(state.Secret, "", "  ")
					if err != nil {
						panic(fmt.Sprintf("Failed to marshal secret: %s", err))
					}
					state.Result = bytes
				}
				return
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if len(state.Prompt) > 0 {
					state.Prompt = state.Prompt[:len(state.Prompt)-1]
					newKeysView(&state)
				}
			case tcell.KeyCtrlU:
				state.Prompt = ""
				newKeysView(&state)
			case tcell.KeyRune:
				state.Prompt += string(ev.Rune())
				newKeysView(&state)
			case tcell.KeyCtrlK, tcell.KeyCtrlP:
				moveUp(&state)
			case tcell.KeyCtrlJ, tcell.KeyCtrlN:
				moveDown(&state)
			}
		}

		screen.Clear()
		drawKeys(state)
		drawScrollbar(state)
		drawStats(state)
		drawPrompt(state)
		drawSecret(state)
		screen.Show()
	}
}

func getKeys(vault vault.Client, entrypoint vault.DirEnt) []string {
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

func recurse(recv chan string, vaultClient vault.Client, entry vault.DirEnt) {
	if !entry.IsDir {
		recv <- entry.Name
		return
	}
	relativeEntries, err := vaultClient.ListDir(entry.Name)
	if err != nil {
		slog.Error("Failed to list directory", "directory", entry.Name, "err", err.Error())
		return
	}
	entries := []vault.DirEnt{}
	for _, sub := range relativeEntries {
		entries = append(entries, vault.DirEnt{
			IsDir: sub.IsDir,
			Name:  entry.Name + sub.Name,
		})
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		wg.Add(1)
		go func(entry vault.DirEnt) {
			defer wg.Done()
			recurse(recv, vaultClient, e)
		}(e)
	}
	wg.Wait()
}

func drawLine(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for _, r := range []rune(text) {
		s.SetContent(x, y, r, nil, style)
		x++
	}
}

func drawKeys(s Ui) {
	yBottom := nKeysToShow(s.Height) - 1
	maxLength := s.Width/2 - 2
	for i, key := range s.FilteredKeys[s.ViewStart:s.ViewEnd] {
		keyToDraw := key
		if len(keyToDraw) > maxLength {
			keyToDraw = fmt.Sprintf("%s..", key[:maxLength-2])
		}
		y := yBottom - i
		if i == s.Cursor {
			drawLine(s.Screen, 0, y, tcell.StyleDefault.Background(tcell.ColorRed), " ")
			drawLine(s.Screen, 1, y, tcell.StyleDefault.Background(tcell.ColorBlack), " ")
			drawLine(s.Screen, 2, y, tcell.StyleDefault.Background(tcell.ColorBlack), keyToDraw)
		} else {
			drawLine(s.Screen, 2, y, tcell.StyleDefault, keyToDraw)
		}
	}
}

func drawScrollbar(s Ui) {
	if len(s.Keys) <= nKeysToShow(s.Height) {
		return
	}
	fullHeight := float32(nKeysToShow(s.Height) - 1)
	nKeys := float32(len(s.Keys))
	normieStartY := float32(s.ViewStart) / nKeys
	normieH := fullHeight / nKeys
	normieEndY := normieStartY + normieH
	startY := int(normieStartY * fullHeight)
	endY := int(normieEndY*fullHeight) + 1
	x := s.Width / 2
	for y := startY; y <= endY; y++ {
		invertedY := int(fullHeight) - y
		s.Screen.SetContent(x, invertedY, '│', nil, tcell.StyleDefault.Foreground(tcell.ColorGray))
	}
}

func drawSecret(s Ui) {
	if reflect.ValueOf(s.Secret).IsZero() {
		return
	}
	x := s.Width/2 + 2
	y := 0
	s.Screen.SetContent(x, y, rune("{"[0]), nil, tcell.StyleDefault)
	y++
	for i := 0; i < 2; i++ {
		keys := []string{}
		name := ""
		var data map[string]interface{}
		if i == 0 {
			data = s.Secret.Data.Data
			for k := range data {
				keys = append(keys, k)
			}
			name = "data"
		} else {
			data = s.Secret.Data.Metadata
			for k := range data {
				keys = append(keys, k)
			}
			name = "metadata"
		}
		slices.Sort(keys)
		drawLine(s.Screen, x+2, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), fmt.Sprintf(`"%s": {`, name))
		y++
		i := 0
		for _, k := range keys {
			v := data[k]
			kStr := fmt.Sprintf(`"%s": `, k)
			drawLine(s.Screen, x+4, y, tcell.StyleDefault.Foreground(tcell.ColorBlue), kStr)
			vStart := x + 4 + len(kStr)
			switch concreteV := v.(type) {
			case string:
				if i < len(data)-1 {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, concreteV))
				} else {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, concreteV))
				}
			case []interface{}:
				if len(concreteV) == 0 {
					if i < len(data)-1 {
						drawLine(s.Screen, vStart, y, tcell.StyleDefault, "[],")
					} else {
						drawLine(s.Screen, vStart, y, tcell.StyleDefault, "[]")
					}
				} else {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault, "[")
					y++
					for i, element := range concreteV {
						if i < len(concreteV)-1 {
							drawLine(s.Screen, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s",`, element.(string)))
						} else {
							drawLine(s.Screen, x+6, y, tcell.StyleDefault.Foreground(tcell.ColorGreen), fmt.Sprintf(`"%s"`, element.(string)))
						}
						y++
					}
					drawLine(s.Screen, x+4, y, tcell.StyleDefault, "],")
				}
			case nil:
				if i < len(data)-1 {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGray), "null,")
				} else {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault.Foreground(tcell.ColorGray), "null")
				}
			default:
				if i < len(data)-1 {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v,", concreteV))
				} else {
					drawLine(s.Screen, vStart, y, tcell.StyleDefault, fmt.Sprintf("%v", concreteV))
				}
			}
			y++
			i++
		}
		s.Screen.SetContent(x+2, y, rune("}"[0]), nil, tcell.StyleDefault)
		y++
	}
	s.Screen.SetContent(x, y, rune("}"[0]), nil, tcell.StyleDefault)
}

func drawStats(s Ui) {
	nKeys := len(s.Keys)
	drawLine(s.Screen, 2, s.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), fmt.Sprintf("%d", nKeys))
}

func drawPrompt(s Ui) {
	drawLine(s.Screen, 0, s.Height-1, tcell.StyleDefault.Bold(true), ">")
	drawLine(s.Screen, 2, s.Height-1, tcell.StyleDefault, s.Prompt)
}

func drawLoadingScreen(s Ui) {
	drawLine(s.Screen, 2, s.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), "Loading...")
}

func nKeysToShow(windowHeight int) int {
	return windowHeight - 2
}

type Match struct {
	Key                string
	ConsecutiveMatches int
}

func newKeysView(s *Ui) {
	matches := []Match{}
	for _, k := range s.Keys {
		if match, consecutive := matchesPrompt(s.Prompt, k); match {
			matches = append(matches, Match{Key: k, ConsecutiveMatches: consecutive})
		}
	}
	slices.SortFunc(matches, func(a, b Match) int {
		return b.ConsecutiveMatches - a.ConsecutiveMatches
	})
	s.FilteredKeys = []string{}
	for _, m := range matches {
		s.FilteredKeys = append(s.FilteredKeys, m.Key)
	}
	s.ViewStart = 0
	s.ViewEnd = min(nKeysToShow(s.Height), len(s.FilteredKeys))
	if len(s.FilteredKeys) == 0 {
		s.Cursor = 0
	} else {
		s.Cursor = min(s.Cursor, len(s.FilteredKeys)-1)
	}
	setSecret(s)
}

func setSecret(s *Ui) {
	if len(s.FilteredKeys) > 0 {
		s.Secret = s.Vault.GetSecret(s.FilteredKeys[s.ViewStart+s.Cursor])
	} else {
		s.Secret = vault.Secret{}
	}
}

func moveUp(s *Ui) {
	if s.ViewStart+s.Cursor+1 < len(s.FilteredKeys) {
		if s.Cursor+1 >= nKeysToShow(s.Height)-SCROLL_OFF && s.ViewEnd < len(s.FilteredKeys) {
			s.ViewStart++
			s.ViewEnd++
		} else {
			s.Cursor++
		}
	}
	setSecret(s)
}

func moveDown(s *Ui) {
	if s.Cursor > 0 {
		if s.Cursor-1 < SCROLL_OFF && s.ViewStart > 0 {
			s.ViewStart--
			s.ViewEnd--
		} else {
			s.Cursor--
		}
	}
	setSecret(s)
}

func matchesPrompt(prompt, s string) (bool, int) {
	if len(prompt) == 0 {
		return true, 0
	}
	prompt = strings.ToLower(prompt)
	s = strings.ToLower(s)
	index := 0
	consecutive := 0
	previousMatched := false
	for _, c := range []byte(s) {
		if c == prompt[index] {
			if previousMatched {
				consecutive++
			}
			previousMatched = true
			if index == len(prompt)-1 {
				return true, consecutive
			}
			index++
		} else {
			previousMatched = false
		}
	}
	return false, 0
}
