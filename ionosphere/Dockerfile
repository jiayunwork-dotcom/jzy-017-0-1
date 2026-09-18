# syntax=docker/dockerfile:1

# ---- build stage: Go 1.22, pure-Go pgx so CGO is off ----
FROM golang:1.22-bookworm AS build
WORKDIR /src

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOFLAGS=-mod=readonly

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- runtime stage ----
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /
COPY --from=build /out/server /server
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/server"]
