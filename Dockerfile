# syntax=docker/dockerfile:1

FROM golang:1.25.3-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/

RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -ldflags="-s -w" -o /out/fleet ./cmd/fleet

FROM scratch

COPY --from=build /out/fleet /fleet

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/fleet"]
