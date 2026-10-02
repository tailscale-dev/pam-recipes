# Build. Nothing outside the standard library, so there is no module download
# step and no go.sum to keep in step.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod main.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /whoami .

# Run. The only reason this is not FROM scratch with nothing else in it is the
# CA bundle, which the server needs to fetch the JWKS over HTTPS.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /whoami /whoami
EXPOSE 8080
ENTRYPOINT ["/whoami"]

# Containers get their own network namespace, so the default of 127.0.0.1
# would be unreachable. Publish with -p 127.0.0.1:8080:8080 to keep the
# listener off the host's public interfaces.
CMD ["--listen", "0.0.0.0:8080"]
