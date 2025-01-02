# Pole (vault)

Browse secrets in vault in the terminal.

![Screenshot](./screenshot.png)

To do it, do:

```sh
go install github.com/slarwise/pole@latest
export VAULT_ADDR=https://my-vault.com
# The vault token is taken from the VAULT_TOKEN env var or from ~/.vault-token
# Logging into vault with `vault login` stores it in ~/.vault-token by default
vault login -method oidc
pole
```

Filter secrets fuzzily by typing letters, navigate secrets and mounts with the arrow keys.

## Development

To start and populate a local vault server, run

```sh
go run dev-vault/main.go
```

Set the environment with

```sh
dev-vault/env.sh
```

and run `go run main.go` to test it.
