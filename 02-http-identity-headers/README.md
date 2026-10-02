# 02 - Internal web apps and forwarded identity

Networks often have a handful of internal apps that either re-implement auth
each time, or just trust that access to the network is enough protection.

This example puts an internal tool behind a PAM HTTP service. PAM authenticates
the user, then tells the application who they are by adding `X-Auth-*` headers
to the request with a signature.

The sample application here is a small Go application that shows the caller what
information was provided to it by PAM, and whether the signature was verified.

## What it models

```mermaid
flowchart LR
    laptop["Engineer<br/>group:engineering"]
    ci["Automation<br/>tag:ci"]
    attacker["Anyone on the host<br/>curl 127.0.0.1:8080"]

    subgraph connector["PAM connector (tag:connector-node)"]
        svc["svc:whoami"]
        app["whoami<br/>127.0.0.1:8080"]
    end

    jwks["signing.border0.io/keys"]

    laptop -->|"signed identity"| svc
    ci -->|"signed, isServiceAccount"| svc
    svc --> app
    attacker -.->|"unsigned, forged"| app
    app -->|"fetch public keys"| jwks
```

The dotted line represents an attacker who has managed to make it onto the
machine hosting our Go service. They could attempt to make a request directly to
the service with forged request headers, to impersonate a real user. Our demo
checks the signature of those headers against the public key published by
Tailscale PAM (previously Border0). This allows the application to then reject
the request.

For more information, see the [header
documentation](https://tailscale.com/docs/privileged-access-management/how-to/access-http-service#pass-tailscale-identity-to-your-web-application).

## Who gets what

| | `svc:whoami` | `tag:connector-node` |
|---|---|---|
| `group:engineering` | HTTP | — |
| `tag:ci` | HTTP, flagged as a service account | — |
| `group:platform` | — | SSH as `ubuntu` |

## Setting it up

1. [Enable the PAM integration][pam-get-started] and create a connector. The
   application runs on the connector host in this example, which keeps the
   recipe to one machine — in practice it would be a separate host the
   connector can reach.
2. Build and run the app on that host:

```console
$ docker build -t whoami ./whoami
$ docker run -d --name whoami -p 127.0.0.1:8080:8080 whoami
```

  Alternatively you can just build and run the Go application directly with `go
  build ./whoami` and run the binary. It listens on `127.0.0.1:8080` by default
  and takes `--listen` and `--jwks-url`.

3. Create an [HTTP service][pam-http] named `whoami` on that connector, with
   `http://127.0.0.1:8080` as the upstream. The name has to match the `svc:`
   reference in the policy file.
4. Apply [`policy.hujson`](./policy.hujson), editing the group membership to
   something real.

Plain HTTP upstream is deliberate. PAM terminates TLS for the user; the hop
from the connector to the application stays on the loopback interface, so
there is no certificate to issue and nothing to trust.

## Trying it out

Open the service from the Tailscale client. You get a green panel, your own
email under **Verified identity**, and the exact canonical string the signature
was checked against.

Now be the dotted line. SSH to the connector host and talk to the application
directly, claiming to be someone more interesting:

```console
$ curl -s -H 'X-Auth-Email: ceo@example.com' http://127.0.0.1:8080/ | jq '.signature.status, .in_signed_header_set'
"unsigned"
[
  {
    "name": "Host",
    "value": "127.0.0.1:8080"
  },
  {
    "name": "X-Auth-Email",
    "value": "ceo@example.com"
  }
]
```

The header is there, the server reports it, and the status says exactly how
much that is worth. An application doing `if email == "ceo@example.com"` would
have been convinced. One that checks the signature first is not.

The same thing happens to a header rewritten in flight: forward a real signed
request but change `X-Auth-Email` on the way past and the status becomes
`invalid` rather than `unsigned`, because the signature no longer matches the
canonical string.

For the service-account path, run the same request from a device tagged
`tag:ci` and watch `X-Auth-IsServiceAccount` come back `true` — inside the
signed set, so an application can refuse to let automation do something that
should need a person.

[pam-get-started]: https://tailscale.com/docs/privileged-access-management/get-started
[pam-http]: https://tailscale.com/docs/privileged-access-management/how-to/access-http-service
