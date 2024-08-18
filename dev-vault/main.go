package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strconv"
	"time"
)

const (
	VAULT_TOKEN = "dev-only-token"
	VAULT_ADDR  = "http://127.0.0.1:8200"
)

var (
	env = []string{
		fmt.Sprintf("VAULT_ADDR=%s", VAULT_ADDR),
		fmt.Sprintf("VAULT_TOKEN=%s", VAULT_TOKEN),
	}
	MOUNTS       = []string{"secret", "secret2", "secret3"}
	SECRET_COUNT = len(KEYS)
)

func logErr(format string, args ...any) {
	format += "\n"
	fmt.Fprintf(os.Stderr, format, args...)
}

func main() {
	if len(os.Args) > 1 {
		count, err := strconv.Atoi(os.Args[1])
		if err != nil {
			logErr("The first argument must be an integer specifying the number of secrets to create, got %s", os.Args[1])
			os.Exit(1)
		}
		if count > SECRET_COUNT {
			logErr("Can have at most %d secrets, got %d", SECRET_COUNT, count)
			os.Exit(1)
		}
		SECRET_COUNT = count
	}
	runVault := exec.Command("vault", "server", "-dev", "-dev-root-token-id", VAULT_TOKEN, "-address", VAULT_ADDR)
	if err := runVault.Start(); err != nil {
		logErr("Failed to start vault %s", err)
		return
	}
	defer func() {
		if err := runVault.Process.Signal(os.Interrupt); err != nil {
			logErr("Failed to stop the vault server: %s", err)
		}
		runVault.Wait()
	}()
	time.Sleep(1 * time.Second)
	for _, mount := range MOUNTS[1:] {
		if err := createMount(mount); err != nil {
			logErr("Failed to create mount: %s", err)
			os.Exit(1)
		}
	}
	fmt.Printf("Created %d mounts: %v\n", len(MOUNTS), MOUNTS)
	for _, key := range KEYS[:SECRET_COUNT] {
		mount := MOUNTS[rand.Intn(len(MOUNTS))]
		data := generateData()
		if err := putSecret(mount, key, data); err != nil {
			logErr("Failed to put secret %s", err)
			os.Exit(1)
		}
	}
	fmt.Printf("Populated the mounts with a total of %d secrets\n", SECRET_COUNT)
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, os.Kill)
	fmt.Printf("Vault is listening on %s and accepting token `%s`\n", VAULT_ADDR, VAULT_TOKEN)
	<-done
}

func putSecret(mount, key string, data map[string]interface{}) error {
	bytes, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("Failed to convert secret data into json, %s, %s", err, string(bytes))
	}
	cmd := exec.Command("vault", "kv", "put", "-mount", mount, key, "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	_, err = stdin.Write(bytes)
	if err != nil {
		return fmt.Errorf("Failed to write data to stdin: %s", err)
	}
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Failed to create secret: %s", output)
	}
	return nil
}

func createMount(name string) error {
	cmd := exec.Command("vault", "secrets", "enable", "-path", name, "-version", "2", "kv")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(string(output))
	}
	return nil
}

var (
	// shuf /usr/share/dict/words | head -150 plus some manuals for hierarchy
	KEYS = []string{
		"calculate/more/infinity",
		"Rhapidophyllum/plant",
		"intellectualist/smart/5head",
		"Scyllaea/okay",
		"excretive/extra/hierarchy",
		"barbasco",
		"tempest",
		"subsinuous",
		"undeficient",
		"chairmaker",
		"trituration",
		"underbody",
		"dipterologist",
		"frailness",
		"funerary",
		"trisilane",
		"carbocinchomeronic",
		"refrain",
		"adobe",
		"suggillation",
		"binodous",
		"Invertebrata",
		"balantidial",
		"dullhead",
		"Caliburn",
		"ilicin",
		"cadmium",
		"Pharaonical",
		"nonuterine",
		"biocoenosis",
		"recountenance",
		"shalloon",
		"croupy",
		"apophantic",
		"accredited",
		"stook",
		"unperflated",
		"synoecism",
		"Didelphidae",
		"superstimulate",
		"Darwinite",
		"Miranda",
		"flauntily",
		"autoschediaze",
		"abortifacient",
		"cytogenic",
		"veratroyl",
		"unclamped",
		"goatly",
		"unchristianness",
		"carbonation",
		"unreverting",
		"owse",
		"topmost",
		"unemployableness",
		"cataclysmically",
		"wheenge",
		"anatropous",
		"veridical",
		"Pterodactyli",
		"scepterless",
		"broadspread",
		"alchemical",
		"drawlink",
		"unbethink",
		"isotopism",
		"alcoholization",
		"prooemion",
		"Aberia",
		"aldine",
		"assurance",
		"cytozoic",
		"thelium",
		"antiprostatic",
		"feltmaker",
		"concavely",
		"Vishal",
		"featherhead",
		"cuminal",
		"tetracolon",
		"assert",
		"Paphian",
		"fountainously",
		"lithium",
		"snoek",
		"theanthropology",
		"Labyrinthula",
		"topographer",
		"surreptitiousness",
		"axonophorous",
		"subchelate",
		"loxodromics",
		"kapur",
		"spiflicated",
		"mnemonize",
		"Lola",
		"ultravirus",
		"noncontent",
		"seditionist",
		"expensiveness",
		"kirombo",
		"subscriver",
		"weaponshowing",
		"gainful",
		"persico",
		"pelage",
		"overlearnedness",
		"syngenesian",
		"preeze",
		"prerefer",
		"kittenish",
		"hirer",
		"gnawingly",
		"unmoist",
		"resubstitute",
		"Actaeon",
		"Leatheroid",
		"unrecoined",
		"pluricentral",
		"misleadingly",
		"pipe",
		"Cycadaceae",
		"upcall",
		"flavid",
		"mothed",
		"rousting",
		"repasser",
		"isonitramine",
		"heroarchy",
		"stomacher",
		"pseudoseptate",
		"oxane",
		"covellite",
		"unscoffing",
		"Brahmi",
		"prolarva",
		"narceine",
		"underpose",
		"depressant",
		"undeviated",
		"meromorphic",
		"alumroot",
		"propound",
		"anthypophora",
		"pomster",
		"Scotlandwards",
		"reschedule",
		"syruplike",
		"Gershom",
		"bronchophthisis",
	}
	DATA = map[string]interface{}{
		"username":   "psy",
		"password":   "hunter2",
		"oopa":       "gangnam",
		"roles":      []string{"reader", "writer", "philantropist"},
		"a":          "bcd, etc",
		"bomb_at":    "2025-09-01",
		"manifest":   "the industrial revolution and its consequences",
		"free":       "palestine",
		"max_memory": 640000,
	}
)

func generateData() map[string]interface{} {
	randomIndices := []int{}
	for range rand.Intn(len(DATA)) + 1 {
		randomIndices = append(randomIndices, rand.Intn(len(DATA)))
	}
	data := map[string]interface{}{}
	i := 0
	for k, v := range DATA {
		if slices.Contains(randomIndices, i) {
			data[k] = v
		}
		i++
	}
	return data
}
