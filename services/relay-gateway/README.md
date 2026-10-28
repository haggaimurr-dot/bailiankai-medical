# relay-gateway

Real-model gateway adapter for the Bailiankai assessment pipeline
(`POST /quality`, `POST /analyze`) on top of an OpenAI-compatible chat
relay with a vision model. Stdlib only.

This file used to live only on the server
(`/opt/bailiankai-webtest/relay_gateway.py` for the test gateway on
127.0.0.1:18087, `/opt/bailiankai/relay_gateway.py` for production on
127.0.0.1:18094). It is versioned here so advice/quality changes go
through `muse-dev` like everything else. The two server copies differed
only in the bind port; the port now comes from `GATEWAY_PORT`
(default 18087).

## Deploy (test only)

```sh
tscp services/relay-gateway/relay_gateway.py ubuntu@42.192.114.201:/opt/bailiankai-webtest/relay_gateway.py
tssh "sudo systemctl restart blk-relay-gateway"
```

The test unit (`blk-relay-gateway`) needs no env change: it binds
18087 by default and reads the key from
`/etc/bailiankai-webtest/relay-key.env` when present.

## Production note

Do NOT deploy this file to `/opt/bailiankai/relay_gateway.py` without
also adding `Environment="GATEWAY_PORT=18094"` to the
`blk-prod-relay-gateway` unit — otherwise the production gateway would
bind 18087 and collide with the test gateway. Production deploys are
out of scope for the test-only workflow; the production key is placed
by the user (see MEMORY.md).

## Tests

```sh
python3 services/relay-gateway/test_retake_advice.py
```
