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

	"github.com/slarwise/pole/internal/vault"

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
	ShowHelp     bool
}

func newUi(vaultClient vault.Client, mounts []string) (Ui, error) {
	screen, err := tcell.NewScreen()
	if err != nil {
		return Ui{}, fmt.Errorf("Failed to create a terminal screen: %s", err)
	}
	if err := screen.Init(); err != nil {
		return Ui{}, fmt.Errorf("Failed to initialize terminal screen: %s", err)
	}
	screen.EnablePaste()
	screen.Clear()
	width, height := screen.Size()
	return Ui{
		Vault:        vaultClient,
		Mounts:       mounts,
		CurrentMount: 0,
		ShowHelp:     true,
		Screen:       screen,
		Width:        width,
		Height:       height,
	}, nil
}

const (
	SCROLL_OFF = 4
)

var (
	STYLE_KEY     = tcell.StyleDefault.Foreground(tcell.ColorBlue)
	STYLE_STRING  = tcell.StyleDefault.Foreground(tcell.ColorGreen)
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
	ui, err := newUi(vaultClient, mounts)
	if err != nil {
		fatal("Failed to initialize UI", "err", err)
	}
	quit := func() {
		// You have to catch panics in a defer, clean up, and
		// re-raise them - otherwise your application can
		// die without leaving any diagnostic trace.
		errorMsg := recover()
		ui.Screen.Fini()
		if errorMsg != nil {
			fmt.Fprintf(os.Stderr, "%s\n", errorMsg)
		} else if len(ui.Result) != 0 {
			fmt.Printf("%s\n", ui.Result)
		}
	}
	defer quit()
	ui.drawPrompt()
	drawLoadingScreen(ui)
	ui.Screen.Show()
	ui.Keys = vaultClient.GetKeys(ui.Mounts[ui.CurrentMount])
	ui.newKeysView()
	ui.Redraw()
	for {
		ev := ui.Screen.PollEvent()
		slog.Info("event", "ev", fmt.Sprintf("%T", ev))
		switch ev := ev.(type) {
		case *tcell.EventResize:
			ui.Screen.Sync()
			ui.Width, ui.Height = ui.Screen.Size()
			ui.ViewEnd = min(nKeysToShow(ui.Height), len(ui.FilteredKeys))
			if ui.ViewStart+ui.Cursor >= ui.ViewEnd {
				ui.Cursor = 0
				ui.ViewStart = 0
			}
		case *tcell.EventKey:
			switch ev.Key() {
			case tcell.KeyEscape, tcell.KeyCtrlC:
				return
			case tcell.KeyEnter:
				if !(reflect.ValueOf(ui.Secret).IsZero()) {
					bytes, err := json.MarshalIndent(ui.Secret, "", "  ")
					if err != nil {
						panic(fmt.Sprintf("Failed to marshal secret: %s", err))
					}
					ui.Result = bytes
				}
				return
			case tcell.KeyBackspace, tcell.KeyBackspace2:
				if len(ui.Prompt) > 0 {
					ui.Prompt = ui.Prompt[:len(ui.Prompt)-1]
					ui.newKeysView()
				}
			case tcell.KeyCtrlU:
				ui.Prompt = ""
				ui.newKeysView()
			case tcell.KeyRune:
				switch ev.Rune() {
				case '?':
					ui.ShowHelp = !ui.ShowHelp
				case ',':
					ui.nextMount()
				case ';':
					ui.previousMount()
				default:
					ui.Prompt += string(ev.Rune())
					ui.newKeysView()
				}
			case tcell.KeyLeft:
				ui.nextMount()
			case tcell.KeyRight:
				ui.previousMount()
			case tcell.KeyCtrlK, tcell.KeyCtrlP, tcell.KeyUp:
				ui.moveUp()
			case tcell.KeyCtrlJ, tcell.KeyCtrlN, tcell.KeyDown:
				ui.moveDown()
				// TODO: Add key for refreshing the secrets
			}
		}

		ui.Redraw()
	}
}

func (u Ui) Redraw() {
	u.Screen.Clear()
	u.drawKeys()
	u.drawScrollbar()
	u.drawStats()
	u.drawHelp()
	u.drawPrompt()
	u.drawSecret()
	u.Screen.Show()
}

func drawLine(s tcell.Screen, x, y int, style tcell.Style, text string) {
	for _, r := range []rune(text) {
		s.SetContent(x, y, r, nil, style)
		x++
	}
}

func (u Ui) drawKeys() {
	yBottom := nKeysToShow(u.Height) - 1
	maxLength := u.Width/2 - 2
	for i, key := range u.FilteredKeys[u.ViewStart:u.ViewEnd] {
		keyToDraw := key
		if len(keyToDraw) > maxLength {
			keyToDraw = fmt.Sprintf("%s..", key[:maxLength-2])
		}
		y := yBottom - i
		if i == u.Cursor {
			drawLine(u.Screen, 0, y, tcell.StyleDefault.Background(tcell.ColorRed), " ")
			drawLine(u.Screen, 1, y, tcell.StyleDefault.Background(tcell.ColorBlack), " ")
			drawLine(u.Screen, 2, y, tcell.StyleDefault.Background(tcell.ColorBlack), keyToDraw)
		} else {
			drawLine(u.Screen, 2, y, tcell.StyleDefault, keyToDraw)
		}
	}
}

func (u Ui) drawScrollbar() {
	if len(u.FilteredKeys) <= nKeysToShow(u.Height) {
		return
	}
	fullHeight := float32(nKeysToShow(u.Height) - 1)
	nKeys := float32(len(u.FilteredKeys))
	normieStartY := float32(u.ViewStart) / nKeys
	normieH := fullHeight / nKeys
	normieEndY := normieStartY + normieH
	startY := int(normieStartY * fullHeight)
	endY := int(normieEndY*fullHeight) + 1
	x := u.Width / 2
	for y := startY; y <= endY; y++ {
		invertedY := int(fullHeight) - y
		u.Screen.SetContent(x, invertedY, '│', nil, tcell.StyleDefault.Foreground(tcell.ColorGray))
	}
}

func (u Ui) drawSecret() {
	if reflect.ValueOf(u.Secret).IsZero() {
		return
	}
	x := u.Width/2 + 2
	y := 0
	drawData(u.Screen, x, &y, "data", u.Secret.Data.Data)
	drawData(u.Screen, x, &y, "metadata", u.Secret.Data.Metadata)
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

func (u Ui) drawStats() {
	nKeysStr := fmt.Sprint(len(u.Keys))
	drawLine(u.Screen, 2, u.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), nKeysStr)
	mountsStr := ""
	for i, m := range u.Mounts {
		if i == u.CurrentMount {
			mountsStr = fmt.Sprintf("%s [%s]", mountsStr, m)
		} else {
			mountsStr = fmt.Sprintf("%s  %s ", mountsStr, m)
		}
	}
	drawLine(u.Screen, 4, u.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), mountsStr)
}

func (u Ui) drawHelp() {
	if !u.ShowHelp {
		return
	}
	helpStr := "Move ↑↓ Change mount ←→ Exit <Esc>"
	drawLine(u.Screen, u.Width/2-len(helpStr)/2+4, u.Height-1, tcell.StyleDefault.Foreground(tcell.ColorRed), helpStr)
}

func (u Ui) drawPrompt() {
	drawLine(u.Screen, 0, u.Height-1, tcell.StyleDefault.Bold(true), ">")
	drawLine(u.Screen, 2, u.Height-1, tcell.StyleDefault, u.Prompt)
}

func drawLoadingScreen(u Ui) {
	drawLine(u.Screen, 2, u.Height-2, tcell.StyleDefault.Foreground(tcell.ColorYellow), fmt.Sprintf("%-*s", u.Width-2, "Loading..."))
}

func nKeysToShow(windowHeight int) int {
	return windowHeight - 2
}

type Match struct {
	Key                string
	ConsecutiveMatches int
}

func (u *Ui) newKeysView() {
	matches := []Match{}
	for _, k := range u.Keys {
		if match, consecutive := matchesPrompt(u.Prompt, k); match {
			matches = append(matches, Match{Key: k, ConsecutiveMatches: consecutive})
		}
	}
	slices.SortFunc(matches, func(a, b Match) int {
		return b.ConsecutiveMatches - a.ConsecutiveMatches
	})
	u.FilteredKeys = []string{}
	for _, m := range matches {
		u.FilteredKeys = append(u.FilteredKeys, m.Key)
	}
	u.ViewStart = 0
	u.ViewEnd = min(nKeysToShow(u.Height), len(u.FilteredKeys))
	if len(u.FilteredKeys) == 0 {
		u.Cursor = 0
	} else {
		u.Cursor = min(u.Cursor, len(u.FilteredKeys)-1)
	}
	u.setSecret()
}

func (u *Ui) setSecret() {
	if len(u.FilteredKeys) > 0 {
		u.Secret = u.Vault.GetSecret(u.Mounts[u.CurrentMount], u.FilteredKeys[u.ViewStart+u.Cursor])
	} else {
		u.Secret = vault.Secret{}
	}
}

func (u *Ui) moveUp() {
	if u.ViewStart+u.Cursor+1 < len(u.FilteredKeys) {
		if u.Cursor+1 >= nKeysToShow(u.Height)-SCROLL_OFF && u.ViewEnd < len(u.FilteredKeys) {
			u.ViewStart++
			u.ViewEnd++
		} else {
			u.Cursor++
		}
	}
	u.setSecret()
}

func (u *Ui) moveDown() {
	if u.Cursor > 0 {
		if u.Cursor-1 < SCROLL_OFF && u.ViewStart > 0 {
			u.ViewStart--
			u.ViewEnd--
		} else {
			u.Cursor--
		}
	}
	u.setSecret()
}

func (u *Ui) nextMount() {
	if len(u.Mounts) < 2 {
		return
	}
	if u.CurrentMount == 0 {
		u.CurrentMount = len(u.Mounts) - 1
	} else {
		u.CurrentMount--
	}
	drawLoadingScreen(*u)
	u.Screen.Show()
	u.Keys = u.Vault.GetKeys(u.Mounts[u.CurrentMount])
	u.Prompt = ""
	u.newKeysView()
}

func (u *Ui) previousMount() {
	if len(u.Mounts) < 2 {
		return
	}
	u.CurrentMount = (u.CurrentMount + 1) % len(u.Mounts)
	drawLoadingScreen(*u)
	u.Screen.Show()
	u.Keys = u.Vault.GetKeys(u.Mounts[u.CurrentMount])
	u.Prompt = ""
	u.newKeysView()
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
