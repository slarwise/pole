# Pole (vault)

Browse secrets in vault in the terminal.

![Screenshot](https://private-user-images.githubusercontent.com/25964718/359639733-495bdcde-a41c-41a2-8be7-9f785cc9f300.png?jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJnaXRodWIuY29tIiwiYXVkIjoicmF3LmdpdGh1YnVzZXJjb250ZW50LmNvbSIsImtleSI6ImtleTUiLCJleHAiOjE3MjQxODY1MDksIm5iZiI6MTcyNDE4NjIwOSwicGF0aCI6Ii8yNTk2NDcxOC8zNTk2Mzk3MzMtNDk1YmRjZGUtYTQxYy00MWEyLThiZTctOWY3ODVjYzlmMzAwLnBuZz9YLUFtei1BbGdvcml0aG09QVdTNC1ITUFDLVNIQTI1NiZYLUFtei1DcmVkZW50aWFsPUFLSUFWQ09EWUxTQTUzUFFLNFpBJTJGMjAyNDA4MjAlMkZ1cy1lYXN0LTElMkZzMyUyRmF3czRfcmVxdWVzdCZYLUFtei1EYXRlPTIwMjQwODIwVDIwMzY0OVomWC1BbXotRXhwaXJlcz0zMDAmWC1BbXotU2lnbmF0dXJlPTRhM2RlYTU5OWI2OTdiNGJlMTJmMDQxMWNiNmE2OWNjMWIzNDRhODRmZDBkMjA0N2ZhYTA5NTBiZWM3YTY1ZTYmWC1BbXotU2lnbmVkSGVhZGVycz1ob3N0JmFjdG9yX2lkPTAma2V5X2lkPTAmcmVwb19pZD0wIn0.ue-7j9fB9FFYTkErwmfK9d_P5IAMjoZuqLvlQaM60us)

To do it, do:

```sh
git clone https://github.com/slarwise/pole
go install
export VAULT_ADDR=https://my-vault.com
export VAULT_TOKEN=secret-token
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
