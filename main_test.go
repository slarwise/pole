package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

const (
	token     = "dev-only-token"
	vaultAddr = "http://127.0.0.1:8200"
)

func TestGetKeys(t *testing.T) {
	vaultServer, err := startVault(token, vaultAddr)
	if err != nil {
		t.Fatalf("Failed to start vault: %s", err)
	}
	defer func() {
		if err := vaultServer.Process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to stop the vault server: %s", err.Error())
		}
		vaultServer.Wait()
	}()
	secrets := map[string]string{
		"/foo":     "a=b",
		"/bar/baz": "c=d",
		"/enterprise/organization/department/unit/team/user/actual-user": "free=palestine",
	}
	if err := populate(vaultAddr, token, secrets); err != nil {
		t.Fatalf("Failed to populate vault with secrets: %s", err.Error())
	}
	vault := VaultClient{
		Addr:  vaultAddr,
		Token: token,
		Mount: "secret",
	}
	entrypoint := DirEnt{
		IsDir: true,
		Name:  "/",
	}
	keys := getKeys(vault, entrypoint)
	if len(keys) != len(secrets) {
		t.Fatalf("Expected %d keys, got %d", len(secrets), len(keys))
	}
	for key := range secrets {
		if !slices.Contains(keys, key) {
			t.Fatalf("Expected %s to be in keys %v", key, keys)
		}
	}
}

func TestGetSecret(t *testing.T) {
	vaultServer, err := startVault(token, vaultAddr)
	if err != nil {
		t.Fatalf("Failed to start vault: %s", err)
	}
	defer func() {
		if err := vaultServer.Process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to stop the vault server: %s", err.Error())
		}
		vaultServer.Wait()
	}()
	secrets := map[string]string{
		"/bar/baz": "c=d",
	}
	if err := populate(vaultAddr, token, secrets); err != nil {
		t.Fatalf("Failed to populate vault with secrets: %s", err.Error())
	}
	vault := VaultClient{
		Addr:  vaultAddr,
		Token: token,
		Mount: "secret",
	}
	secret, err := vault.getSecret("/bar/baz")
	if err != nil {
		t.Fatalf("Got unexpected error: %s", err)
	}
	data, found := secret.Data.Data["c"]
	if !found || data != "d" {
		t.Fatalf("Expected secret to have data `c=d`, got %v", secret.Data.Data)
	}
}

func TestMatchesPrompt(t *testing.T) {
	tests := map[string]struct {
		prompt      string
		key         string
		match       bool
		consecutive int
	}{
		"match": {
			prompt:      "set",
			key:         "secret",
			match:       true,
			consecutive: 1,
		},
		"match2": {
			prompt:      "seet",
			key:         "secret",
			match:       true,
			consecutive: 2,
		},
		"exact-match": {
			prompt:      "secret",
			key:         "secret",
			match:       true,
			consecutive: 5,
		},
		"short-match": {
			prompt:      "user",
			key:         "/user",
			match:       true,
			consecutive: 3,
		},
		"long-match": {
			prompt:      "user",
			key:         "/if/you/can/read/the/full/path/of/this/key/you/are/the/person/in/the/red/flag/monitor/meme",
			match:       true,
			consecutive: 0,
		},
		"case-insensitive-match": {
			prompt:      "user",
			key:         "UsEr",
			match:       true,
			consecutive: 3,
		},
		"no-match": {
			prompt:      "asdf",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
		"no-match1": {
			prompt:      "a",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
		"no-match2": {
			prompt:      "seeet",
			key:         "secret",
			match:       false,
			consecutive: 0,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			match, consecutive := matchesPrompt(test.prompt, test.key)
			if match != test.match {
				t.Fatalf("Expected %s to match %s", test.prompt, test.key)
			}
			if consecutive != test.consecutive {
				t.Fatalf("Expected %d consecutive character matches for prompt %s and key %s, got %d", test.consecutive, test.prompt, test.key, consecutive)
			}
		})
	}
}

func startVault(token, addr string) (*exec.Cmd, error) {
	cmd := exec.Command("vault", "server", "-dev", "-dev-root-token-id", token, "-address", addr)
	if err := cmd.Start(); err != nil {
		return cmd, err
	}
	time.Sleep(1 * time.Second)
	return cmd, nil
}

func populate(vaultAddr, token string, secrets map[string]string) error {
	for key, data := range secrets {
		cmd := exec.Command("vault", "kv", "put",
			"-mount", "secret",
			key, data)
		cmd.Env = []string{
			fmt.Sprintf("VAULT_ADDR=%s", vaultAddr),
			fmt.Sprintf("VAULT_TOKEN=%s", token),
		}
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("Failed to create secret: %s", output)
		}
	}
	return nil
}
