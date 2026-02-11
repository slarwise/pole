package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/slarwise/pole/internal/vault"

	"github.com/gdamore/tcell/v2"
)

func fatal(format string, args ...any) {
	format += "\n"
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}

type Ui struct {
	Screen        tcell.Screen
	Keys          []string
	FilteredKeys  []string
	Secret        vault.Secret
	Prompt        string
	ViewStart     int
	ViewEnd       int
	Cursor        int
	Width         int
	Height        int
	Result        []byte
	Vault         vault.Client
	Mounts        []string
	CurrentMount  int
	ShowHelp      bool
	SelectedField int
	ShowSecret    bool
	Keybinds      Keybinds
}

func newUi(vaultClient vault.Client, mounts []string, keybinds Keybinds) (Ui, error) {
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
		Keybinds:     keybinds,
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

type Keybinds struct {
	Keys  map[tcell.Key]func(u *Ui)
	Runes map[rune]func(u *Ui)
}

var defaultKeybinds = Keybinds{
	Keys: map[tcell.Key]func(u *Ui){
		tcell.KeyCtrlO: func(u *Ui) { u.openInBrowser() },
		tcell.KeyLeft:  func(u *Ui) { u.previousMount() },
		tcell.KeyRight: func(u *Ui) { u.nextMount() },
		tcell.KeyCtrlJ: func(u *Ui) { u.moveDown() },
		tcell.KeyCtrlK: func(u *Ui) { u.moveUp() },
		tcell.KeyCtrlN: func(u *Ui) { u.moveSelectedFieldDown() },
		tcell.KeyCtrlP: func(u *Ui) { u.moveSelectedFieldUp() },
		tcell.KeyCtrlI: func(u *Ui) { u.toggleShowSecret() },
		tcell.KeyCtrlR: func(u *Ui) { u.refreshSecrets() },
	},
	Runes: map[rune]func(u *Ui){
		'?': func(u *Ui) { u.ShowHelp = !u.ShowHelp },
		',': func(u *Ui) { u.nextMount() },
		';': func(u *Ui) { u.previousMount() },
		' ': func(u *Ui) { u.copyCurrentField() },
	},
}

var keybindCtrlPattern = regexp.MustCompile(`^c-([a-z])$`)

func readKeybinds(configPath string) (Keybinds, error) {
	if configPath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return Keybinds{}, fmt.Errorf("get home dir: %v", err)
		}
		configPath = filepath.Join(homeDir, ".config", "pole", "config")
		if _, err := os.Stat(configPath); err != nil {
			return defaultKeybinds, nil
		}
	}
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return Keybinds{}, fmt.Errorf("read %v: %v", configPath, err)
	}
	keybinds := Keybinds{}
	keybinds.Keys = map[tcell.Key]func(u *Ui){}
	keybinds.Runes = map[rune]func(u *Ui){}
	lines := strings.FieldsFunc(string(bytes), func(r rune) bool { return r == '\n' })
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return Keybinds{}, fmt.Errorf("expected each line to be on the form <key><space><value>, got %v", line)
		}
		keybind, action := fields[0], fields[1]
		var f func(u *Ui)
		switch action {
		case "open-in-browser":
			f = func(u *Ui) { u.openInBrowser() }
		case "prev-mount":
			f = func(u *Ui) { u.previousMount() }
		case "next-mount":
			f = func(u *Ui) { u.nextMount() }
		case "next-secret":
			f = func(u *Ui) { u.moveDown() }
		case "prev-secret":
			f = func(u *Ui) { u.moveUp() }
		case "next-field":
			f = func(u *Ui) { u.moveSelectedFieldDown() }
		case "prev-field":
			f = func(u *Ui) { u.moveSelectedFieldUp() }
		case "toggle-show-secrets":
			f = func(u *Ui) { u.toggleShowSecret() }
		case "refresh-secrets":
			f = func(u *Ui) { u.refreshSecrets() }
		case "toggle-show-help":
			f = func(u *Ui) { u.ShowHelp = !u.ShowHelp }
		case "copy-selected-field":
			f = func(u *Ui) { u.copyCurrentField() }
		default:
			return Keybinds{}, fmt.Errorf("got unknown action `%v`. See https://github.com/slarwise/pole/tree/main?tab=readme-ov-file#keybindings for the available actions.", action)
		}

		keybind = strings.ToLower(keybind)
		if keybind == "space" {
			keybinds.Runes[' '] = f
		} else if len(keybind) == 1 {
			keybinds.Runes[rune(keybind[0])] = f
		} else if keybindCtrlPattern.MatchString(keybind) {
			match := keybindCtrlPattern.FindStringSubmatch(keybind)[1]
			asciiValue := byte(match[0])
			keybinds.Keys[tcell.Key(asciiValue-96)] = f
		} else if keybind == "up" {
			keybinds.Keys[tcell.KeyUp] = f
		} else if keybind == "down" {
			keybinds.Keys[tcell.KeyDown] = f
		} else if keybind == "right" {
			keybinds.Keys[tcell.KeyRight] = f
		} else if keybind == "left" {
			keybinds.Keys[tcell.KeyLeft] = f
		} else if keybind == "page-up" {
			keybinds.Keys[tcell.KeyPgUp] = f
		} else if keybind == "page-down" {
			keybinds.Keys[tcell.KeyPgDn] = f
		} else if keybind == "tab" {
			keybinds.Keys[tcell.KeyTab] = f
		} else {
			return Keybinds{}, fmt.Errorf("unknown keybind: `%v`. See https://github.com/slarwise/pole/tree/main?tab=readme-ov-file#keybindings for available keybindings.", keybind)
		}
	}

	return keybinds, nil
}

func main() {
	configPath := flag.String("config", "", "The path to the config file")
	flag.Parse()
	keybinds, err := readKeybinds(*configPath)
	if err != nil {
		fatal("Read keybind config: %v", err)
	}
	log.SetFlags(0) // Disable the timestamp
	vaultClient, err := vault.NewClient()
	if err != nil {
		fatal("Failed to create vault client: %v", err)
	}
	mounts, err := vaultClient.GetMounts()
	if err != nil {
		fatal("Failed to get mounts: %v", err)
	}
	if len(os.Getenv("DEBUG")) > 0 {
		logFile, err := os.Create("./log")
		if err != nil {
			fatal("Failed to create log file: %v", err)
		}
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	} else {
		log.SetOutput(io.Discard)
	}
	if len(flag.Args()) > 0 && flag.Arg(0) == "list" {
		if err := listSecrets(vaultClient, mounts, flag.Args()[1:]); err != nil {
			fatal("list secrets: %s", err)
		}
		return
	}
	ui, err := newUi(vaultClient, mounts, keybinds)
	if err != nil {
		fatal("Failed to initialize UI: %v", err)
	}
	quit := func() {
		// You have to catch panics in a defer, clean up, and
		// re-raise them - otherwise your application can
		// die without leaving any diagnostic trace.
		maybePanic := recover()
		ui.Screen.Fini()
		if maybePanic != nil {
			panic(maybePanic)
		}
		if len(ui.Result) != 0 {
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
					var buf bytes.Buffer
					encoder := json.NewEncoder(&buf)
					encoder.SetEscapeHTML(false)
					encoder.SetIndent("", "  ")
					if err := encoder.Encode(ui.Secret); err != nil {
						panic(fmt.Sprintf("Failed to marshal secret: %s", err))
					}
					ui.Result = buf.Bytes()
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
				if action, found := ui.Keybinds.Runes[ev.Rune()]; found {
					action(&ui)
				} else {
					ui.Prompt += string(ev.Rune())
					ui.newKeysView()
				}
			default:
				if action, found := ui.Keybinds.Keys[ev.Key()]; found {
					action(&ui)
				}
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
	drawData(u.Screen, x, &y, "data", u.Secret.Data.Data, u.SelectedField, u.ShowSecret)
	drawData(u.Screen, x, &y, "metadata", u.Secret.Data.Metadata, -1, true)
}

func drawData(s tcell.Screen, x int, y *int, name string, data map[string]interface{}, highlightedIndex int, showField bool) {
	keys := []string{}
	for k := range data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	kToDraw := fmt.Sprintf(`%s: `, name)
	drawLine(s, x, *y, STYLE_KEY, kToDraw)
	*y++
	for i, k := range keys {
		kToDraw := fmt.Sprintf(`%s: `, k)
		styleKey := STYLE_KEY
		styleString := STYLE_STRING
		styleDefault := STYLE_DEFAULT
		styleNull := STYLE_NULL
		if i == highlightedIndex {
			drawLine(s, x, *y, tcell.StyleDefault.Background(tcell.ColorRed), " ")
			drawLine(s, x+1, *y, tcell.StyleDefault.Background(tcell.ColorBlack), " ")
			styleKey = STYLE_KEY.Background(tcell.ColorBlack)
			styleString = STYLE_STRING.Background(tcell.ColorBlack)
			styleDefault = STYLE_DEFAULT.Background(tcell.ColorBlack)
			styleNull = STYLE_NULL.Background(tcell.ColorBlack)
		}
		drawLine(s, x+2, *y, styleKey, kToDraw)
		vStart := x + 2 + len(kToDraw)
		var v interface{}
		if showField {
			v = data[k]
		} else {
			v = "*******"
		}
		switch vForReal := v.(type) {
		case string:
			drawLine(s, vStart, *y, styleString, vForReal)
			*y++
		case []interface{}:
			if len(vForReal) == 0 {
				drawLine(s, vStart, *y, styleDefault, "[]")
			} else {
				*y++
				for _, e := range vForReal {
					drawLine(s, x+4, *y, styleDefault, "- ")
					drawLine(s, x+6, *y, styleString, e.(string))
					*y++
				}
			}
		case nil:
			drawLine(s, vStart, *y, styleNull, "null")
			*y++
		default:
			drawLine(s, vStart, *y, styleDefault, fmt.Sprintf("%v", vForReal))
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
	helpStr := "Move ↑↓ Change mount ←→ Change field C-[N/P] Copy <Space> Exit <Esc>"
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
	u.SelectedField = 0
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
	u.SelectedField = 0
	u.setSecret()
}

func (u *Ui) previousMount() {
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

func (u *Ui) nextMount() {
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

func (u *Ui) openInBrowser() {
	url := u.Secret.Url
	cmd := exec.Command("open", url)
	if err := cmd.Run(); err != nil {
		slog.Error("Failed to open secret in browser", "err", err, "url", url)
	}
}

func (u *Ui) moveSelectedFieldDown() {
	u.SelectedField = min(max(0, len(u.Secret.Data.Data)-1), u.SelectedField+1)
}

func (u *Ui) moveSelectedFieldUp() {
	u.SelectedField = max(0, u.SelectedField-1)
}

func (u *Ui) toggleShowSecret() {
	u.ShowSecret = !u.ShowSecret
}

func (u Ui) copyCurrentField() {
	keys := []string{}
	for k := range u.Secret.Data.Data {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	val := u.Secret.Data.Data[keys[u.SelectedField]]
	u.Screen.SetClipboard([]byte(fmt.Sprint(val)))
}

func (u *Ui) refreshSecrets() {
	u.Vault.ClearSecretsCache()
	u.setSecret()
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

func listSecrets(vaultClient vault.Client, mounts []string, args []string) error {
	mount := mounts[0]
	if len(args) > 0 {
		mount = args[0]
		if !slices.Contains(mounts, mount) {
			return fmt.Errorf("unknown mount `%s`. Available mounts: %v", mount, mounts)
		}
	}
	keys := vaultClient.GetKeys(mount)
	for _, key := range keys {
		fmt.Println(key)
	}
	return nil
}
