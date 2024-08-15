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
	Mounts       []string
	CurrentMount int
}

const (
	SCROLL_OFF = 4
)

var (
	STYLE_KEY     = tcell.StyleDefault.Foreground(tcell.ColorBlue)
	STYLE_STRING  = tcell.StyleDefault.Foreground(tcell.ColorPink)
	STYLE_NULL    = tcell.StyleDefault.Foreground(tcell.ColorGray)
	STYLE_DEFAULT = tcell.StyleDefault
)

func main() {
	log.SetFlags(0) // Disable the timestamp
	vaultClient := vault.Client{
		Addr:  mustGetEnv("VAULT_ADDR"),
		Token: mustGetEnv("VAULT_TOKEN"),
	}
	mounts := []string{}
	mounts = vaultClient.GetMounts()
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
		Screen:       screen,
		Vault:        vaultClient,
		Mounts:       mounts,
		CurrentMount: 0,
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
	state.Keys = vault.GetKeys(vaultClient, state.Mounts[state.CurrentMount])
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
			case tcell.KeyCtrlO:
				state.CurrentMount = (state.CurrentMount + 1) % len(state.Mounts)
				state.Keys = vault.GetKeys(state.Vault, state.Mounts[state.CurrentMount])
				state.Prompt = ""
				newKeysView(&state)
			case tcell.KeyCtrlI:
				if state.CurrentMount == 0 {
					state.CurrentMount = len(state.Mounts) - 1
				} else {
					state.CurrentMount--
				}
				state.Keys = vault.GetKeys(state.Vault, state.Mounts[state.CurrentMount])
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
	drawData(s.Screen, x, &y, "data", s.Secret.Data.Data)
	drawData(s.Screen, x, &y, "metadata", s.Secret.Data.Metadata)
}

func drawData(s tcell.Screen, x int, y *int, name string, data map[string]interface{}) {
	keys := []string{}
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	kToDraw := fmt.Sprintf(`%s: `, name)
	drawLine(s, x, *y, STYLE_KEY, kToDraw)
	*y++
	for _, k := range keys {
		kToDraw := fmt.Sprintf(`%s: `, k)
		drawLine(s, x+2, *y, STYLE_KEY, kToDraw)
		vStart := x + 2 + len(kToDraw)
		v := data[k]
		switch vForReal := v.(type) {
		case string:
			drawLine(s, vStart, *y, STYLE_STRING, vForReal)
			*y++
		case []interface{}:
			if len(vForReal) == 0 {
				drawLine(s, vStart, *y, STYLE_DEFAULT, "[]")
			} else {
				*y++
				for _, e := range vForReal {
					drawLine(s, x+4, *y, STYLE_DEFAULT, "- ")
					drawLine(s, x+6, *y, STYLE_STRING, e.(string))
					*y++
				}
			}
		case nil:
			drawLine(s, vStart, *y, tcell.StyleDefault.Foreground(tcell.ColorGray), "null")
			*y++
		default:
			drawLine(s, vStart, *y, tcell.StyleDefault, fmt.Sprintf("%v", vForReal))
			*y++
		}
	}
}

func drawStats(s Ui) {
	nKeysStr := fmt.Sprint(len(s.Keys))
	drawLine(s.Screen, 2, s.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), nKeysStr)
	mountsStr := ""
	for i, m := range s.Mounts {
		if i == s.CurrentMount {
			mountsStr = fmt.Sprintf("%s [%s]", mountsStr, m)
		} else {
			mountsStr = fmt.Sprintf("%s  %s ", mountsStr, m)
		}
	}
	drawLine(s.Screen, 4, s.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), mountsStr)
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
		s.Secret = s.Vault.GetSecret(s.Mounts[s.CurrentMount], s.FilteredKeys[s.ViewStart+s.Cursor])
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
