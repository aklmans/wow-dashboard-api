FROM golang:1.26.3-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /bin/wow-dashboard-api \
    ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /bin/wow-dashboard-api /wow-dashboard-api

EXPOSE 7272

ENTRYPOINT ["/wow-dashboard-api"]