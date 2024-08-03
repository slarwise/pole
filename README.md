# Pole (vault)

```
mountA
  dirA
    secret1
  secret2
```

Read all secrets under mountA concurrently.

```
LIST mountA
  LIST dirA
    GET dirA/secret1
  GET secret2
```

Wait until this is done to visualize the data.
