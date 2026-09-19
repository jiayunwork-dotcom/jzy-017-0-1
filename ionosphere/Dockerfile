# syntax=docker/dockerfile:1

FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go vet ./... && go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ionod .

FROM alpine:3.20
RUN adduser -D -u 10001 iono
COPY --from=build /out/ionod /usr/local/bin/ionod
USER iono
EXPOSE 8080
ENTRYPOINT ["ionod"]
