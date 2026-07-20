# Deployment and remote access

## Local mock profile

Run `./maintainctl bootstrap` then `./maintainctl up`. The UI binds to `127.0.0.1:8080`; internal services publish no host ports. Use `./maintainctl down` to stop it.

## Remote access

SSH forwarding is the simplest supported remote path:

```sh
ssh -N -L 8080:127.0.0.1:8080 operator@maintainer-host
```

Then browse `http://127.0.0.1:8080` locally.

For TLS plus OIDC or passkeys, configure a trusted authentication gateway exposing `/verify`, provide a certificate/key, and start the pinned Caddy profile:

```sh
export MAINTAINER_REMOTE_HOST=maintainer.example.internal
export OIDC_GATEWAY_URL=https://auth.example.internal
export MAINTAINER_TLS_CERT=/absolute/path/cert.pem
export MAINTAINER_TLS_KEY=/absolute/path/key.pem
docker compose -f compose.yaml -f compose.remote.yaml --profile remote up -d remote-proxy
```

The default remote bind remains loopback. Set `MAINTAINER_REMOTE_BIND` deliberately only behind a firewall. Remote identity headers do not replace the appliance's local RBAC/session boundary.

## Production execution

Set absolute `MAINTAINER_DATA_ROOT`, immutable `MODEL_MANIFEST_ROOT`, immutable `RUNNERD_POLICY_FILE`, and a dedicated rootless worker-daemon socket in `RUNNERD_WORKER_SOCKET`. Validate with `docker compose -f compose.yaml -f compose.production.yaml config`. The host's general Docker socket is unsupported. Production backup startup also requires `secrets/backup.key`, mode `0600`, containing an unpadded base64 encoding of exactly 32 random bytes.
