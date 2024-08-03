package main

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"
)

func TestGetKeys(t *testing.T) {
	token := "dev-only-token"
	vaultAddr := "http://127.0.0.1:8200"
	cmd := exec.Command("vault", "server", "-dev", "-dev-root-token-id", token, "-address", vaultAddr)
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start the vault server: %s", err.Error())
	}
	defer func() {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			t.Logf("Failed to stop the vault server: %s", err.Error())
		}
		cmd.Wait()
	}()
	time.Sleep(1 * time.Second)
	secrets := map[string]string{
		"/foo":     "a=b",
		"/bar/baz": "c=d",
		"/enterprise/organization/department/unit/team/user/actual-user": "free=palestine",
	}
	if err := populate(t, vaultAddr, token, secrets); err != nil {
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

func populate(t *testing.T, vaultAddr, token string, secrets map[string]string) error {
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
		t.Logf("Created secret: %s", output)
	}
	return nil
}
